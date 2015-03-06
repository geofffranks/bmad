// Bolo Monitoring and Analytics - a collection of functions and datastructures
// to support the Bolo Monitoring and Analytics Daemon (bmad). Contains all the
// bmad logic for configuration management, check execution, and data submission.
package bma

import "github.com/geofffranks/bmad/log"
import "launchpad.net/goyaml"
import "io/ioutil"
import "math/rand"
import "net"
import "os"
import "path/filepath"
import shellwords "github.com/mattn/go-shellwords"
import "strings"
import "time"

var cfg *Config

const MIN_INTERVAL int64 = 10

// Config objects represent the internal bmad configuration,
// after being loaded from the YAML config file.
type Config struct {
	Send_bolo   string                // Command to use for spawning the send_bolo process, to submit Check results
	Every       int64                 // Global default interval to run Checks (in seconds)
	Retry_every int64                 // Global default interval to retry failed Checks (in seconds)
	Retries     int                   // Global default number of times to retry a failed Check
	Timeout     int64                 // Global default timeout for maximum check execution time (in seconds)
	Checks      map[string]*Check     // Map describing all Checks to be executed via bmad, keyed by Check name
	Env         map[string]string     // Global default environment variables to apply to all Checks run
	Log         log.LogConfig      // Configuration for the bmad logger
	Host        string                // Hostname that bmad is running on
	Include_dir string                // Directory to include *.conf files from
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
	cfg.Include_dir = "/etc/bmad.d"

	return &cfg
}

// Performes heuristics to determine the hostname of the current host.
// Tries os.Hostname(), and if that isn't fully qualified (contains a '.'),
// Fails over to finding the first hostname for the first IP of the host
// that contains a '.'. If none do, fails back to the unqualified hostname.
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

// Loads a YAML config file specified by cfg_file, and returns
// a Config object representing that config. Config reloads are
// auto-detected and handled seemlessly.
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

	log.SetupLogging(new_cfg.Log)

	if new_cfg.Include_dir != "" {
		files, err := filepath.Glob(new_cfg.Include_dir + "/*.conf")
		if err != nil {
			log.Warn("Couldn't find include files: %s", err.Error())
		} else {
			for _, file := range(files) {
				source, err := ioutil.ReadFile(file)
				if err != nil {
					log.Warn("Couldn't read %q: %s", file, err.Error())
					continue
				}

				checks := map[string]*Check{}
				err = goyaml.Unmarshal(source, &checks)
				if err != nil {
					log.Warn("Could not parse yaml from %q: %s", file, err.Error())
					continue
				}

				for name, check := range(checks) {
					if _, exists := new_cfg.Checks[name]; exists {
						log.Warn("Check %q defined in multiple config files, ignoring definition in %s", name, file)
						continue
					}
					new_cfg.Checks[name] = check
				}
			}
		}
	}

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
	return cfg, nil
}
