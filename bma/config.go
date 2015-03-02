package bma

import "github.com/geofffranks/bmad/logger"
import "launchpad.net/goyaml"
import "io/ioutil"
import "math/rand"
import "os"
import shellwords "github.com/mattn/go-shellwords"
import "time"

var log *logger.Logger
var cfg *Config

const MIN_INTERVAL int64 = 10

type Config struct {
	Send_bolo   string
	Interval    int64
	Retry       int64
	Timeout     int64
	Max_attempts int
	Checks      map[string]*Check
	Env         map[string]string
	Log         logger.LogConfig

	host        string
}
//FIXME: test config reloading + merging of schedule data

func default_config() (*Config) {
	var cfg Config
	cfg.Interval    = 300
	cfg.Retry       = 60
	cfg.Checks      = map[string]*Check{}
	cfg.Max_attempts = 1
	cfg.Timeout     = 45
	cfg.Send_bolo   = "send_bolo -t stream"
	cfg.Env         = map[string]string{}

	h, err  := os.Hostname()
	if err != nil {
		panic(err)
	}
	cfg.host = h

	return &cfg
}

func LoadConfig(cfg_file string) (*Config, error) {
	new_cfg := default_config()

	source, err := ioutil.ReadFile(cfg_file)
	if err != nil {
		return cfg, err
	}

	err = goyaml.Unmarshal(source, &new_cfg)
	if err != nil {
		return cfg, err
	}

	new_log := logger.Create(new_cfg.Log)
	if (err != nil) {
		if (log != nil) {
			log.Error("Couldn't load logging configuration: %s", err)
		}
		return cfg, err
	}
	log = new_log

	for name, check := range new_cfg.Checks {
		if check.Name == "" {
			check.Name = name
		}
		if check.Command == "" {
			log.Error("Unspecified command for %s - ignoring check", check.Name)
			continue
		} else {
			check.cmd_args, err = shellwords.Parse(check.Command)
			if err != nil {
				log.Error("Couldn't parse %s's command `%s` into arguments: %q - ignoring check",
					check.Name, check.Command, err)
			}
		}
		if check.Interval <= 0 {
			check.Interval = new_cfg.Interval
		} else if check.Interval <= MIN_INTERVAL {
			check.Interval = MIN_INTERVAL
		}
		if check.Retry <= 0 {
			check.Retry = new_cfg.Retry
		}
		if check.Retry > check.Interval {
			check.Retry = check.Interval
		}
		if check.Max_attempts <= 0 {
			check.Max_attempts = new_cfg.Max_attempts
		}
		if check.Timeout <= 0 {
			check.Timeout = new_cfg.Timeout
		}
		if check.Env == nil {
			check.Env = new_cfg.Env
		} else {
			for env, val := range new_cfg.Env {
				if _, ok := check.Env[env]; !ok {
					check.Env[env] = val
				}
			}
		}

		check.next_run = time.Now().Add(time.Duration(rand.Int63n(check.Interval * int64(time.Second))))
		if (cfg != nil) {
			if val, ok := cfg.Checks[check.Name]; ok {
				check.next_run   = val.next_run
				check.started_at = val.started_at
				check.ended_at   = val.ended_at
				check.duration   = val.duration
				check.attempts   = val.attempts
				check.rc         = val.rc
				check.latency    = val.latency
				check.stdout     = val.stdout
				check.stderr     = val.stderr
				check.sig_term   = val.sig_term
				check.sig_kill   = val.sig_kill
				check.process    = val.process
			}
		}
	}

	cfg = new_cfg
	log.Debug("Config successfully loaded as: %#v", cfg)
	log.Debug("Logger: %#v", log)
	return cfg, nil
}

func Logger() (*logger.Logger) {
	return log
}
