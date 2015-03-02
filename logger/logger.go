package logger

import "fmt"
import "log/syslog"
import "strings"

type LogConfig struct {
	Level    string
	Facility string
}

type Logger struct {
	level syslog.Priority
	log   *syslog.Writer
}

//FIXME: support console and file based logging, a la libvigor

func Create (cfg LogConfig) (*Logger) {
	facility := get_facility(cfg.Facility)
	logger, err := syslog.New(facility, "")
	if err != nil {
		panic(err)
	}

	var l Logger
	l.log   = logger
	l.level = get_level(cfg.Level)
	return &l
}

func (self *Logger) Debug (msg string, args ...interface{}) {
	if self.level >= syslog.LOG_DEBUG {
		self.log.Debug(fmt.Sprintf(msg, args...))
	}
}

func (self *Logger) Info (msg string, args ...interface{}) {
	if self.level >= syslog.LOG_INFO {
		self.log.Info(fmt.Sprintf(msg, args...))
	}
}

func (self *Logger) Notice (msg string, args ...interface{}) {
	if self.level >= syslog.LOG_NOTICE {
		self.log.Notice(fmt.Sprintf(msg, args...))
	}
}

func (self *Logger) Warn (msg string, args ...interface{}) {
	if self.level >= syslog.LOG_WARNING {
		self.log.Warning(fmt.Sprintf(msg, args...))
	}
}

func (self *Logger) Error (msg string, args ...interface{}) {
	if self.level >= syslog.LOG_ERR {
		self.log.Err(fmt.Sprintf(msg, args...))
	}
}

func (self *Logger) Crit (msg string, args ...interface{}) {
	if self.level >= syslog.LOG_CRIT {
		self.log.Crit(fmt.Sprintf(msg, args...))
	}
}

func (self *Logger) Alert (msg string, args ...interface{}) {
	if self.level >= syslog.LOG_ALERT {
		self.log.Alert(fmt.Sprintf(msg, args...))
	}
}

func (self *Logger) Emerg (msg string, args ...interface{}) {
	if self.level >= syslog.LOG_EMERG {
		self.log.Emerg(fmt.Sprintf(msg, args...))
	}
}

func get_level (level string) (syslog.Priority) {
	var priority syslog.Priority
	switch strings.ToLower(level) {
	case "debug":
		priority = priority | syslog.LOG_DEBUG
	case "info":
		priority = priority | syslog.LOG_INFO
	case "notice":
		priority = priority | syslog.LOG_NOTICE
	case "warning":
		priority = priority | syslog.LOG_WARNING
	case "warn":
		priority = priority | syslog.LOG_WARNING
	case "error":
		priority = priority | syslog.LOG_ERR
	case "err":
		priority = priority | syslog.LOG_ERR
	case "crit":
		priority = priority | syslog.LOG_CRIT
	case "alert":
		priority = priority | syslog.LOG_ALERT
	case "emerg":
		priority = priority | syslog.LOG_EMERG
	default:
		panic(fmt.Sprintf("Unsupported logging priority %q", level))
	}

	return priority
}

func get_facility (facility string) (syslog.Priority) {
	var priority syslog.Priority
	switch strings.ToLower(facility) {
	case "kern":
		priority = syslog.LOG_KERN
	case "user":
		priority = syslog.LOG_USER
	case "mail":
		priority = syslog.LOG_MAIL
	case "daemon":
		priority = syslog.LOG_DAEMON
	case "auth":
		priority = syslog.LOG_AUTH
	case "syslog":
		priority = syslog.LOG_SYSLOG
	case "lpr":
		priority = syslog.LOG_LPR
	case "news":
		priority = syslog.LOG_NEWS
	case "uucp":
		priority = syslog.LOG_UUCP
	case "cron":
		priority = syslog.LOG_CRON
	case "authpriv":
		priority = syslog.LOG_AUTHPRIV
	case "ftp":
		priority = syslog.LOG_FTP
	case "local0":
		priority = syslog.LOG_LOCAL0
	case "local1":
		priority = syslog.LOG_LOCAL1
	case "local2":
		priority = syslog.LOG_LOCAL2
	case "local3":
		priority = syslog.LOG_LOCAL3
	case "local4":
		priority = syslog.LOG_LOCAL4
	case "local5":
		priority = syslog.LOG_LOCAL5
	case "local6":
		priority = syslog.LOG_LOCAL6
	case "local7":
		priority = syslog.LOG_LOCAL7
	default:
		panic(fmt.Sprintf("Unsupported logging priority %q", facility))
	}

	return priority
}
