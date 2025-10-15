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

	logger.Infof("windows-logging: Initialized with configuration: LogType=%s, MaxEntries=%d, Logs=%s",
		conf.LogType, conf.MaxEntries, conf.Logs)

	return s, nil
}

func (s *windowsLoggingLogging) Name() resource.Name {
	return s.name
}

// Readings decides between live mode or test mode.
func (s *windowsLoggingLogging) Readings(ctx context.Context, extra map[string]interface{}) (map[string]interface{}, error) {
	s.logger.Infof("windows-logging: Readings called for %s with config LogType=%s, MaxEntries=%d, Logs=%s",
		s.name, s.cfg.LogType, s.cfg.MaxEntries, s.cfg.Logs)

	// TEST MODE
	if s.cfg.Logs == "test" || strings.HasSuffix(s.cfg.Logs, ".csv") || strings.HasSuffix(s.cfg.Logs, ".json") {
		s.logger.Info("windows-logging: Entering TEST mode")
		return readTestLogs(s.cfg.Logs, s.logger)
	}

	// LIVE MODE
	s.logger.Infof("windows-logging: Entering LIVE mode for log type: %s", s.cfg.LogType)
	return readLiveLogs(s.cfg.LogType, s.cfg.MaxEntries, s.logger)
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

//
// ------------------------
// Helper Functions
// ------------------------
//

// readTestLogs parses test data from CSV or JSON
func readTestLogs(logPath string, logger logging.Logger) (map[string]interface{}, error) {
	filePath := logPath
	if logPath == "test" {
		filePath = "example_logs/000009999-synth 1.csv"
	}

	logger.Infof("windows-logging: Loading test log file: %s", filePath)
	data, err := parseTestLogFile(filePath)
	if err != nil {
		logger.Errorf("windows-logging: Failed to parse test log file %s: %v", filePath, err)
		return nil, err
	}

	testLogs, ok := data["test_logs"].([]map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("invalid test log data format")
	}

	logger.Infof("windows-logging: Successfully read %d test log entries", len(testLogs))
	return map[string]interface{}{
		"state": "test_mode",
		"logs":  testLogs,
	}, nil
}

// readLiveLogs reads real Windows Event Log entries
func readLiveLogs(logType string, maxEntries int, logger logging.Logger) (map[string]interface{}, error) {
	el, err := eventlog.Open(logType)
	if err != nil {
		logger.Errorf("windows-logging: Failed to open event log '%s': %v", logType, err)
		return map[string]interface{}{
			"state":  "error",
			"error":  err.Error(),
			"source": logType,
		}, nil
	}
	defer el.Close()

	records, err := el.Read(eventlog.Backwards, 0)
	if err != nil {
		logger.Errorf("windows-logging: Failed to read events from %s: %v", logType, err)
		return map[string]interface{}{
			"state": "error",
			"error": err.Error(),
		}, nil
	}

	var entries []map[string]interface{}
	for i, rec := range records {
		if i >= maxEntries {
			break
		}
		entry := map[string]interface{}{
			"TimeGenerated": rec.TimeGenerated.Format(time.RFC3339),
			"SourceName":    rec.Source,
			"EventID":       rec.EventID,
			"EventType":     rec.Type,
			"Message":       strings.TrimSpace(rec.Message),
		}
		entries = append(entries, entry)
	}

	if len(entries) == 0 {
		entries = append(entries, map[string]interface{}{
			"TimeGenerated": time.Now().Format(time.RFC3339),
			"SourceName":    logType,
			"EventID":       1001,
			"EventType":     "Information",
			"Message":       "No recent Windows events found or insufficient permissions.",
		})
	}

	logger.Infof("windows-logging: Returning %d live entries", len(entries))
	return map[string]interface{}{
		"state":        "live_mode",
		"windows_logs": entries,
	}, nil
}

// parseTestLogFile supports CSV or JSON test input
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
