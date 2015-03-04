package bma

import "github.com/geofffranks/bmad/logger"
import "launchpad.net/goyaml"
import "io/ioutil"
import "math/rand"
import "net"
import "os"
import shellwords "github.com/mattn/go-shellwords"
import "strings"
import "time"

var log *logger.Logger
var cfg *Config

const MIN_INTERVAL int64 = 10

type Config struct {
	Send_bolo   string
	Every       int64
	Retry_every int64
	Timeout     int64
	Retries     int
	Checks      map[string]*Check
	Env         map[string]string
	Log         logger.LogConfig
	Host        string
}
//FIXME: test config reloading + merging of schedule data

func default_config() (*Config) {
	var cfg Config
	cfg.Every       = 300
	cfg.Retry_every = 60
	cfg.Checks      = map[string]*Check{}
	cfg.Retries     = 1
	cfg.Timeout     = 45
	cfg.Send_bolo   = "send_bolo -t stream"
	cfg.Env         = map[string]string{}
	cfg.Host        = hostname()

	return &cfg
}

func hostname() (string) {
	h, err  := os.Hostname()
	if err != nil {
		log.Error("Couldn't get hostname for current host: %s", err.Error())
		return "unknown"
	}
	if strings.ContainsRune(h, '.') {
		return h
	}
	addrs, err := net.LookupHost(h)
	if err != nil {
//		log.Warn("Couldn't resolve FQDN of host: %s", err.Error());
		return h
	}
	if len(addrs) > 0 {
		names, err := net.LookupAddr(addrs[0])
		if err != nil {
	//		log.Warn("Couldn't resolve FQDN of host: %s", err.Error());
			return h
		}
		for _, name := range(names) {
			if strings.ContainsRune(name, '.') {
				return name
			}
		}
	}

	log.Warn("No FQDN resolvable, defaulting to unqualified hostname")
	return h
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
		if check.Every <= 0 {
			check.Every = new_cfg.Every
		} else if check.Every <= MIN_INTERVAL {
			check.Every = MIN_INTERVAL
		}
		if check.Retry_every <= 0 {
			check.Retry_every = new_cfg.Retry_every
		}
		if check.Retry_every > check.Every {
			check.Retry_every = check.Every
		}
		if check.Retries <= 0 {
			check.Retries = new_cfg.Retries
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

		check.next_run = time.Now().Add(time.Duration(rand.Int63n(check.Every * int64(time.Second))))
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
