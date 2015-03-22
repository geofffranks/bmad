// The Bolo Monitoring and Analytics Daemon
//
// bmad is an agent designed to execute monitoring
// and analytics checks at periodic configured intervals,
// submitting results up to a Bolo server.
//
//OPTIONS
//
// --config, -c FILE
//		Specify an alternate config file. Defaults to /etc/bmad.conf
// --test, -t
//		Test mode - runs all checks immediately in the foreground
// --match, -m REGEX
//		Filters the checks to run in --test mode
// --noop, -n
//		Disable result submission to bolo (only used for --test mode)
// --help, -h
//		Displays the help dialog
//
// Profiling:
//
// --cpuprofile
//		Enables CPU Profiling
// --memprofile
//		Enables memory Profiling
// --blockprofile
//		Enables block/contention Profiling
//
//
// CONFIGURATION
//
// bmad configs are YAML config files, describing global configuration, as
// well as all the checks that should be run. Below is an example configuration
// with all available directives filled in with their defaults:
//
//	send_bolo:   send_bolo -t stream    # Command for spawning the send_bolo result submission process
//	every:       300                    # Default interval to run checks (in seconds)
//	retry_every: 60                     # Default interval to retry failed checks (in seconds)
//	retries:     1                      # Default number of times to retry failed checks before submitting results
//	timeout:     45                     # Default maximum execution time (in seconds) of a check
//	bulk:        false                  # Default for is this check a bulk check? (must be "true" to enable)
//	report:      false                  # Default for automatically report status of the bulk check execution? (must be "true" to enable)
//	env:         {}                     # Hash of environment variables to set when running checks
//	host:        <local FQDN>           # hostname that bmad is running on (will auto-detect FQDN if possible)
//	include_dir: /etc/bmad.d            # Directory to load additional check configurations from
//	checks:      {}                     # Hash of checks to run
//	log:
//		type:      console                # Specifies whether to log to stdout/console, syslog, or file
//		level:     debug                  # Log level to use (debug, info, notice, warn, err, etc)
//		facility:  daemon                 # syslog facility (only used in syslog mode)
//		file:      ""                     # File name to log to (only used in file mode)
//
// Any files ending in '.conf' in the include_dir directory will be automatically loaded as additional
// hashes of check configurations, which are merged in with any found in the main config file. If there
// are any duplicate check names found, the earliest seen takes precedence.
//
// CHECKS
//
// Running checks is the primary purpose of bmad. Checks are scheduled, and run. Once complete, their
// standard output is captured and sent up to bolo. In addition to this, metadata regarding the check
// execution will automatically be sent up to bolo, to enable easier monitoring of the monitoring system.
// If the check is a bulk check, and is configured to do so, its return code will be processed, and a
// STATE result will be sent up to bolo as well.
//
// As you may have surmised by now, checks come in two flavors: regular, and bulk. Regular checks are
// single events, usually reporting the STATE of a specific thing and perhaps some related COUNTERS or SAMPLES.
// Bulk checks generally run a whole bunch of tests, and submit a large amount of performance data up
// to bolo. Bulk checks are always run at their regular interval, and circumvent bmad's retry logic,
// As mentioned above, they also can be configured to have their success/failure submitted up as a STATE
// message.
//
// Checks share many directives as the main config file, to override the global defaults. Any check-specific
// settings take precedence over the global values (since that's generally what one would expect of an override).
// For the case of environment variables, the hash of environment variables is merged together, with any
// conflicts being chosen in favor of the check-specific value. Below are all the available check configuration
// directives:
//
//	my_check:                             # name of the check
//		command:     /path/to/cmd --args    # command to run
//		every:       300                    # Interval to run this check (in seconds)
//		retries:     1                      # Number of times to retry after failure, before submitting results
//		retry_every: 60                     # Interval to retry the check after failure
//		timeout:     45                     # Maximum execution time (in seconds) of the check
//		env:         {}                     # Hash of environment variables to set for the check
//		run_as:      root                   # User to run the check as (defaults to the user running bmad)
//		bulk:        false                  # Is this check a bulk check? See CHECKS for details (must be "true" to enable)
//		report:      false                  # Automatically report status of the bulk check execution? (must be "true" to enable)
//		name:        my_check               # Override the name specified by the key of this check
//
// For proper retry and status submission, checks must exit with an exit code that indicates its STATE,
// according to the following values:
//
//	0    OK
//	1    WARNING
//	2    CRITICAL
//	3    UNKNOWN
//
// REAL WORLD EXAMPLE
//
// Here's a real world example of /etc/bmad.conf:
//
//	send_bolo:   /usr/bin/send_bolo -t stream -e tcp://bolo.example.com:2999
//	log:
//		level:     warning
//		facility:  daemon
//		type:      syslog
//	checks:
//		hostinfo:
//			command: /usr/lib/bolo/collectors/hostinfo
//			every:   3600
//			bulk:    true
//
// And an example /etc/bmad.d/sar.conf
//
//	sar:
//		command:   /usr/lib/bolo/collectors/sar
//		every:     15
//		timeout:   10
//		bulk:      true
//
// NOTE: the /etc/bmad.d/sar.conf file has checks defined at the top level of the file, and does not
// contain a 'checks' key, like /etc/bmad.conf.
//
// AUTHOR
//
// Written by Geoff Franks <geoff.franks@gmail.com>
//
package main

import "github.com/davecheney/profile"
import "github.com/geofffranks/bmad/bma"
import "github.com/geofffranks/bmad/log"
import "code.google.com/p/getopt"
import "fmt"
import "os"
import "os/signal"
import "regexp"
import "strings"
import "syscall"
import "time"

const TICK time.Duration = 100 * time.Millisecond

var cfg *bma.Config

func main() {
	getopt.StringLong(  "config",       'c', "/etc/bmad.conf", "specifies alternative config file.", "/etc/bmad.conf")
	getopt.BoolLong(    "test",         't',                   "ignore scheduling, and execute one run of all matching checks sequentially")
	getopt.StringLong(  "match"  ,      'm', ".",              "regex for filtering checks for --test mode")
	getopt.BoolLong(    "noop",         'n',                   "disable result submission to bolo (only used for --test mode)")
	getopt.BoolLong(    "help",         'h',                   "display help dialog")
	getopt.BoolLong(    "cpuprofile",    0,                    "Enables memory profiling")
	getopt.BoolLong(    "memprofile",    0,                    "Enables memory profiling")
	getopt.BoolLong(    "blockprofile",  0,                    "Enables memory profiling")

	getopt.DisplayWidth = 80
	getopt.HelpColumn   = 30
	getopt.Parse()
	if getopt.GetValue("help") == "true" {
		getopt.Usage()
		os.Exit(1)
	}

	if getopt.GetValue("cpuprofile") == "true" {
		defer profile.Start(profile.CPUProfile).Stop()
	}
	if getopt.GetValue("memprofile") == "true" {
		defer profile.Start(profile.MemProfile).Stop()
	}
	if getopt.GetValue("blockprofile") == "true" {
		defer profile.Start(profile.BlockProfile).Stop()
	}

	var err error
	cfg, err = bma.LoadConfig(getopt.GetValue("config"))
	if err != nil {
		panic(fmt.Sprintf("Couldn't parse config file %s: %s", getopt.GetValue("config"), err.Error()))
	}

	log.Notice("bmad starting up")
	err = bma.ConnectToBolo()
	if err != nil {
		log.Error("Couldn't spawn send_bolo: %s", err.Error())
		panic(fmt.Sprintf("Couldn't spawn send_bolo: %s", err.Error()))
	}

	if getopt.GetValue("test") == "true" {
		var ran int
		for _, check := range(cfg.Checks) {
			want, err := regexp.Match(getopt.GetValue("match"), []byte(check.Name))
			if err != nil {
				panic(err)
			}
			if !want {
				continue
			}

			ran++
			run_once(check)

		}
		fmt.Printf("---------------------------\n")
		fmt.Printf("Found and ran %d checks matching `%s`\n", ran, getopt.GetValue("match"))
		fmt.Printf("---------------------------\n")
	} else {
		run_loop()
	}
}

func run_once(check *bma.Check) () {
	fmt.Printf("---------------------------\n")
	fmt.Printf("Executing %s in --test mode\n", check.Name)
	fmt.Printf("---------------------------\n")
	if err := check.Spawn(); err != nil {
		fmt.Printf("Error executing %s: %s", check.Name, err.Error())
	}
	complete := check.Reap();
	for !complete {
		time.Sleep(TICK)
		complete = check.Reap();
	}
	fmt.Printf("Results:\n")
	for _, line := range(strings.Split(check.Output(), "\n")) {
		fmt.Printf("	%s\n", line)
	}
	if getopt.GetValue("noop") != "true" {
		fmt.Printf("Sending results to bolo...")
		if err := check.Submit(false); err != nil {
			fmt.Printf("Error submitting results: %s\n", err.Error)
		} else {
			fmt.Printf("Ok\n")
		}
	} else {
		fmt.Printf("no-op mode enabled. Skipping check result submission\n")
	}
	fmt.Printf("\n")
}

func run_loop() () {
	var in_flight [](*bma.Check)
	var CFG_DUMP   bool
	var CFG_RELOAD bool
	var SHUTDOWN   bool

	sig_chan := make(chan os.Signal, 1)
	signal.Notify(sig_chan, syscall.SIGUSR1, syscall.SIGHUP, os.Interrupt, os.Kill, syscall.SIGTERM, syscall.SIGINT)
	go func () {
		for {
			sig := <-sig_chan
			switch sig {
			case syscall.SIGUSR1:
				CFG_DUMP   = true
			case syscall.SIGHUP:
				CFG_RELOAD = true
			default:
				SHUTDOWN   = true
			}
		}
	}()

	for {
		if SHUTDOWN {
			log.Info("Shutdown requested")
			bma.DisconnectFromBolo()
			break
		}
		if CFG_RELOAD {
			log.Info("Configuration reload requested")
			var err error
			cfg, err = bma.LoadConfig(getopt.GetValue("config"))
			if err != nil {
				log.Error("Couldn't reload config: %s", err.Error())
			}
			CFG_RELOAD = false
			time.Sleep(250 * time.Millisecond) // make sure any buffers get read/sent/cleared for send_bolo
			bma.DisconnectFromBolo()
			err = bma.ConnectToBolo()
			if err != nil {
				log.Error("Couldn't spawn send_bolo: %s", err.Error())
				panic(fmt.Sprintf("Couldn't spawn send_bolo: %s", err.Error()))
			}

		}
		if CFG_DUMP {
			log.Info("Configuration dump requested")
			log.Warn("Configuration dumping unsupported")
			//FIXME: implement config dumping
			CFG_DUMP = false
		}
		for _, check := range cfg.Checks {
			if check.ShouldRun() {
				log.Debug("Spawning check \"%s\"", check.Name)
				if err := check.Spawn(); err != nil {
					log.Error("Error spawning check \"%s\": %s", check.Name, err.Error())
					continue
				}
				in_flight = append(in_flight, check)
			}
		}

		var still_running [](*bma.Check)
		for _, check := range in_flight {
			if complete := check.Reap(); !complete {
				still_running = append(still_running, check)
			} else {
				log.Debug("%s reaped successfully", check.Name)
				if err := check.Submit(true); err != nil {
					log.Error("Error submitting check results for %s: %s", check.Name, err.Error())
				}
			}
		}
		in_flight = still_running

		time.Sleep(TICK)
	}
}
