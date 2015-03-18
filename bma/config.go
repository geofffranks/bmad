// Bolo Monitoring and Analytics - a collection of functions and datastructures
// to support the Bolo Monitoring and Analytics Daemon (bmad). Contains all the
// bmad logic for configuration management, check execution, and data submission.
package bma

import "errors"
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
	Bulk        string                // Global default for is this a bulk-mode check
	Report      string                // Global default for should a bulk check report its STATE
	Checks      map[string]*Check     // Map describing all Checks to be executed via bmad, keyed by Check name
	Env         map[string]string     // Global default environment variables to apply to all Checks run
	Log         log.LogConfig         // Configuration for the bmad logger
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

var os_hostname = func() (string, error) {
	return os.Hostname()
}
var net_lookuphost = func(h string) ([]string, error) {
	return net.LookupHost(h)
}
var net_lookupaddr = func(a string) ([]string, error) {
	return net.LookupAddr(a)
}

// Performes heuristics to determine the hostname of the current host.
// Tries os.Hostname(), and if that isn't fully qualified (contains a '.'),
// Fails over to finding the first hostname for the first IP of the host
// that contains a '.'. If none do, fails back to the unqualified hostname.
func hostname() (string) {
	h, err  := os_hostname()
	if err != nil {
		log.Error("Couldn't get hostname for current host: %s", err.Error())
		return "unknown"
	}
	if strings.ContainsRune(h, '.') {
		return h
	}
	addrs, err := net_lookuphost(h)
	if err != nil {
//		log.Warn("Couldn't resolve FQDN of host: %s", err.Error());
		return h
	}
	if len(addrs) > 0 {
		names, err := net_lookupaddr(addrs[0])
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

	if new_cfg.Include_dir != "" {
		log.Debug("Loading auxillary configs from %s", new_cfg.Include_dir)
		files, err := filepath.Glob(new_cfg.Include_dir + "/*.conf")
		if err != nil {
			log.Warn("Couldn't find include files: %s", err.Error())
		} else {
			for _, file := range(files) {
				log.Debug("Loading auxillary config: %s", file)
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
		if err :=initialize_check(name, check, new_cfg); err != nil {
			log.Error("Invalid check config for %s: %s (skipping)", name, err.Error())
			delete(new_cfg.Checks, name)
			continue
		}

		if (cfg != nil) {
			if val, ok := cfg.Checks[check.Name]; ok {
				merge_checks(check, val)
			}
		}
		log.Debug("Check %s defined as %#v", check.Name, check)
	}

	cfg = new_cfg
	log.SetupLogging(cfg.Log)
	log.Debug("Config successfully loaded as: %#v", cfg)
	return cfg, nil
}

var first_run = func (interval int64) (time.Time) {
	return time.Now().Add(time.Duration(rand.Int63n(interval * int64(time.Second))))
}

func merge_checks(check *Check, old *Check) {
	check.next_run   = old.next_run
	check.started_at = old.started_at
	check.ended_at   = old.ended_at
	check.duration   = old.duration
	check.attempts   = old.attempts
	check.rc         = old.rc
	check.latency    = old.latency
	check.stdout     = old.stdout
	check.stderr     = old.stderr
	check.sig_term   = old.sig_term
	check.sig_kill   = old.sig_kill
	check.process    = old.process
}

func initialize_check(name string, check *Check, defaults *Config) (error) {
	if check.Name == "" {
		check.Name = name
	}
	if check.Command == "" {
		return errors.New("unspecified command")
	} else {
		var err error
		check.cmd_args, err = shellwords.Parse(check.Command)
		if err != nil {
			log.Error("Couldn't parse %s's command `%s` into arguments: %q - ignoring check",
				check.Name, check.Command, err)
		}
	}
	if check.Every <= 0 {
		check.Every = defaults.Every
	} else if check.Every <= MIN_INTERVAL {
		check.Every = MIN_INTERVAL
	}
	if check.Every <= 0 {
		check.Every = MIN_INTERVAL * 30
	}
	if check.Retry_every <= 0 {
		check.Retry_every = defaults.Retry_every
	}
	if check.Retry_every > check.Every {
		check.Retry_every = check.Every
	}
	if check.Retries <= 0 {
		check.Retries = defaults.Retries
	}
	if check.Timeout <= 0 {
		check.Timeout = defaults.Timeout
	}
	if check.Timeout >= check.Retry_every || check.Timeout <= 0 {
		check.Timeout = check.Retry_every - 1
	}
	if check.Timeout <= 0 {
		check.Timeout = MIN_INTERVAL - 1
	}
	if check.Bulk == "" {
		check.Bulk = defaults.Bulk
	}
	if check.Report == "" {
		check.Report = defaults.Report
	}
	if check.Env == nil {
		check.Env = defaults.Env
	} else {
		for env, val := range defaults.Env {
			if _, ok := check.Env[env]; !ok {
				check.Env[env] = val
			}
		}
	}
	check.next_run = first_run(check.Every)
	return nil
}
