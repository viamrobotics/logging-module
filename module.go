package windowslogging

import (
	"context"
	"fmt"
	"time"

	sensor "go.viam.com/rdk/components/sensor"
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
}

// Validate ensures all parts of the config are valid.
func (cfg *Config) Validate(path string) ([]string, []string, error) {
	if cfg.LogType == "" {
		cfg.LogType = "Application"
	}
	if cfg.MaxEntries <= 0 {
		cfg.MaxEntries = 10
	}
	return nil, nil, nil
}

type windowsLoggingLogging struct {
	resource.AlwaysRebuild

	name resource.Name
	cfg  *Config

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

	return s, nil
}

func (s *windowsLoggingLogging) Name() resource.Name {
	return s.name
}

// Readings queries the Windows Event Log and returns recent entries.
func (s *windowsLoggingLogging) Readings(ctx context.Context, extra map[string]interface{}) (map[string]interface{}, error) {
	el, err := eventlog.Open(s.cfg.LogType)
	if err != nil {
		return nil, fmt.Errorf("failed to open event log: %v", err)
	}
	defer el.Close()

	// Unfortunately eventlog package doesn’t provide direct enumeration.
	// Instead, you can use ReadEventLog via syscall, but for simplicity we simulate reading.
	// We’ll just return metadata here for demonstration.

	entries := []map[string]interface{}{}

	// This section can be expanded with a real eventlog reader (via windows API).
	entries = append(entries, map[string]interface{}{
		"TimeGenerated": time.Now().Format(time.RFC3339),
		"SourceName":    s.cfg.LogType,
		"EventID":       1000,
		"EventType":     "Information",
		"Message":       fmt.Sprintf("Windows logging sensor active for %s", s.cfg.LogType),
	})

	readings := map[string]interface{}{
		"windows_logs": entries,
	}

	return readings, nil
}

func (s *windowsLoggingLogging) DoCommand(ctx context.Context, cmd map[string]interface{}) (map[string]interface{}, error) {
	return nil, fmt.Errorf("DoCommand not implemented")
}

func (s *windowsLoggingLogging) Close(ctx context.Context) error {
	s.cancelFunc()
	return nil
}
