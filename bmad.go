package main

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
	getopt.StringLong("config", 'c', "/etc/bmad.conf", "specifies alternative config file.", "/etc/bmad.conf")
	getopt.BoolLong("test", 't', "ignore scheduling, and execute one run of all matching checks sequentially")
	getopt.StringLong("match", 'm', ".", "regex for filtering checks for --test mode")
	getopt.BoolLong("noop", 'n', "disable result submission to bolo (only used for --test mode)")
	getopt.BoolLong("help", 'h', "display help dialog")

	getopt.DisplayWidth = 80
	getopt.HelpColumn   = 30
	getopt.Parse()
	if getopt.GetValue("help") == "true" {
		getopt.Usage()
		os.Exit(1)
	}

	var err error
	cfg, err = bma.LoadConfig(getopt.GetValue("config"))
	if err != nil {
		panic(fmt.Sprintf("Couldn't parse config file %s: %s", getopt.GetValue("config"), err))
	}

	log.Notice("bmad starting up")
	bma.ConnectToBolo()
	//FIXME: tests galore

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
	output, complete := check.Reap();
	for !complete {
		time.Sleep(TICK)
		output, complete = check.Reap();
	}
	fmt.Printf("Results:\n")
	for _, line := range(strings.Split(output, "\n")) {
		fmt.Printf("	%s\n", line)
	}
	if getopt.GetValue("noop") != "true" {
		fmt.Printf("Sending results to bolo...")
		if err := bma.SendToBolo(output); err != nil {
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
			if output, complete := check.Reap(); !complete {
				still_running = append(still_running, check)
			} else {
				log.Debug("%s reaped successfully", check.Name)
				if err := bma.SendToBolo(output); err != nil {
					log.Error("Error submitting check results for %s: %s", check.Name, err.Error())
				}
			}
		}
		in_flight = still_running

		time.Sleep(TICK)
	}
}
