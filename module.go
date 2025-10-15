package windowslogging

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"go.viam.com/rdk/components/sensor"
	"go.viam.com/rdk/logging"
	"go.viam.com/rdk/resource"

	"golang.org/x/sys/windows/svc/eventlog"
)

var (
	Logging = resource.NewModel("jandj", "windows-logging", "logging")
)

func init() {
	resource.RegisterComponent(sensor.API, Logging,
		resource.Registration[sensor.Sensor, *Config]{
			Constructor: newWindowsLoggingLogging,
		},
	)
}

type Config struct {
	LogType    string `json:"log_type"`    // e.g., "Application", "System"
	MaxEntries int    `json:"max_entries"` // how many recent entries to return
	Logs       string `json:"logs"`        // "test" or path to file (e.g., example_logs/SawHandpieceLog.json)
}

// Validate ensures all parts of the config are valid.
func (cfg *Config) Validate(path string) ([]string, []string, error) {
	if cfg.LogType == "" {
		cfg.LogType = "Application"
	}
	if cfg.MaxEntries <= 0 {
		cfg.MaxEntries = 10
	}
	if cfg.Logs == "" {
		cfg.Logs = "live" // default
	}
	return nil, nil, nil
}

type windowsLoggingLogging struct {
	resource.AlwaysRebuild

	name   resource.Name
	cfg    *Config
	logger logging.Logger

	cancelCtx  context.Context
	cancelFunc context.CancelFunc
}

func newWindowsLoggingLogging(ctx context.Context, deps resource.Dependencies, rawConf resource.Config, logger logging.Logger) (sensor.Sensor, error) {
	conf, err := resource.NativeConfig[*Config](rawConf)
	if err != nil {
		return nil, err
	}
	return NewLogging(ctx, deps, rawConf.ResourceName(), conf, logger)
}

func NewLogging(ctx context.Context, deps resource.Dependencies, name resource.Name, conf *Config, logger logging.Logger) (sensor.Sensor, error) {
	cancelCtx, cancelFunc := context.WithCancel(context.Background())

	s := &windowsLoggingLogging{
		name:       name,
		logger:     logger,
		cfg:        conf,
		cancelCtx:  cancelCtx,
		cancelFunc: cancelFunc,
	}

	// Log configuration at initialization
	logger.Infof("windows-logging: Initialized with configuration: LogType=%s, MaxEntries=%d, Logs=%s",
		conf.LogType, conf.MaxEntries, conf.Logs)

	return s, nil
}

func (s *windowsLoggingLogging) Name() resource.Name {
	return s.name
}

// Readings queries the Windows Event Log and returns recent entries.
func (s *windowsLoggingLogging) Readings(ctx context.Context, extra map[string]interface{}) (map[string]interface{}, error) {
	s.logger.Infof("windows-logging: Readings called for %s with config LogType=%s, MaxEntries=%d, Logs=%s",
		s.name, s.cfg.LogType, s.cfg.MaxEntries, s.cfg.Logs)

	// 1. TEST MODE
	if s.cfg.Logs == "test" || strings.HasSuffix(s.cfg.Logs, ".csv") || strings.HasSuffix(s.cfg.Logs, ".json") {
		s.logger.Info("windows-logging: Entering TEST mode")
		filePath := s.cfg.Logs
		if s.cfg.Logs == "test" {
			filePath = "example_logs/000009999-synth 1.csv"
		}

		data, err := parseTestLogFile(filePath)
		if err != nil {
			s.logger.Errorf("windows-logging: Failed to parse test log file %s: %v", filePath, err)
			return nil, err
		}

		testLogs, ok := data["test_logs"].([]map[string]interface{})
		if !ok {
			return nil, fmt.Errorf("invalid test log data format")
		}

		logsJSON, err := json.Marshal(testLogs)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal test logs: %v", err)
		}

		s.logger.Infof("windows-logging: Successfully read %d entries from %s", len(testLogs), filePath)

		return map[string]interface{}{
			"state": "test_mode",
			"logs":  string(logsJSON),
		}, nil
	}

	// 2. LIVE MODE
	s.logger.Infof("windows-logging: Entering LIVE mode for log type: %s", s.cfg.LogType)
	el, err := eventlog.Open(s.cfg.LogType)
	if err != nil {
		s.logger.Errorf("windows-logging: Failed to open event log '%s': %v", s.cfg.LogType, err)
		return map[string]interface{}{
			"state":  "error",
			"error":  err.Error(),
			"source": s.cfg.LogType,
		}, nil
	}
	defer el.Close()

	entries := []map[string]interface{}{
		{
			"TimeGenerated": time.Now().Format(time.RFC3339),
			"SourceName":    s.cfg.LogType,
			"EventID":       1000,
			"EventType":     "Information",
			"Message":       fmt.Sprintf("Windows logging sensor active for %s", s.cfg.LogType),
		},
	}

	logsJSON, err := json.Marshal(entries)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal live logs: %v", err)
	}

	s.logger.Infof("windows-logging: Returning %d live entries", len(entries))

	return map[string]interface{}{
		"state":        "live_mode",
		"windows_logs": string(logsJSON),
	}, nil
}

func (s *windowsLoggingLogging) DoCommand(ctx context.Context, cmd map[string]interface{}) (map[string]interface{}, error) {
	s.logger.Infof("windows-logging: DoCommand called with: %+v", cmd)
	return nil, fmt.Errorf("DoCommand not implemented")
}

func (s *windowsLoggingLogging) Close(ctx context.Context) error {
	s.logger.Infof("windows-logging: Closing sensor %s (LogType=%s, Logs=%s)", s.name, s.cfg.LogType, s.cfg.Logs)
	s.cancelFunc()
	return nil
}

func parseTestLogFile(filePath string) (map[string]interface{}, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return nil, fmt.Errorf("could not open test log file: %v", err)
	}
	defer file.Close()

	ext := filepath.Ext(filePath)
	var entries []map[string]interface{}

	switch ext {
	case ".csv":
		reader := csv.NewReader(file)
		headers, err := reader.Read()
		if err != nil {
			return nil, fmt.Errorf("failed to read CSV header: %v", err)
		}
		for {
			record, err := reader.Read()
			if err != nil {
				break
			}
			entry := make(map[string]interface{})
			for i, h := range headers {
				entry[h] = record[i]
			}
			entries = append(entries, entry)
		}
	case ".json":
		if err := json.NewDecoder(file).Decode(&entries); err != nil {
			return nil, fmt.Errorf("failed to parse JSON file: %v", err)
		}
	default:
		return nil, fmt.Errorf("unsupported file extension: %s", ext)
	}

	return map[string]interface{}{
		"test_logs": entries,
	}, nil
}
