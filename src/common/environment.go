package common

import (
	"database/sql"
	"errors"
	"html/template"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	_ "github.com/mattn/go-sqlite3" // SQLite driver

	"github.com/dragonrider23/verbose"
)

// Environment holds "global" application information such as a database connection,
// logging, the config, sessions, etc.
type Environment struct {
	DB     *DatabaseAccessor
	Config *Config
	Views  *Views
	Dev    bool
	Log    *Logger
}

func NewEnvironment(dev bool) *Environment {
	return &Environment{Dev: dev}
}

func NewTestEnvironment() *Environment {
	return &Environment{
		Config: NewEmptyConfig(),
		Log:    NewEmptyLogger(),
	}
}

type DatabaseAccessor struct {
	*sql.DB
}

func NewDatabaseAccessor(config *Config) (*DatabaseAccessor, error) {
	var db *sql.DB
	var err error

	if config.Database.Type == "sqlite" {
		db, err = sql.Open("sqlite3", config.Database.Address)
	} else {
		return nil, errors.New("Unsupported database type " + config.Database.Type)
	}

	if err != nil {
		return nil, err
	}
	return &DatabaseAccessor{db}, nil
}

type Views struct {
	source string
	t      *template.Template
	e      *Environment
}

func NewViews(e *Environment, basepath string) (v *Views, err error) {
	defer func() {
		if r := recover(); r != nil {
			switch x := r.(type) {
			case string:
				err = errors.New(x)
			case error:
				err = x
			default:
				err = errors.New("Unknown panic")
			}
		}
	}()

	tmpl := template.New("").Funcs(template.FuncMap{
		"dict": func(values ...interface{}) (map[string]interface{}, error) {
			if len(values)%2 != 0 {
				return nil, errors.New("invalid dict call")
			}
			dict := make(map[string]interface{}, len(values)/2)
			for i := 0; i < len(values); i += 2 {
				key, ok := values[i].(string)
				if !ok {
					return nil, errors.New("dict keys must be strings")
				}
				dict[key] = values[i+1]
			}
			return dict, nil
		},
		"list": func(values ...interface{}) ([]interface{}, error) {
			return values, nil
		},
	})

	filepath.Walk(basepath, func(path string, info os.FileInfo, err1 error) error {
		if strings.HasSuffix(path, ".tmpl") {
			if _, err := tmpl.ParseFiles(path); err != nil {
				panic(err)
			}
		}
		return nil
	})
	v = &Views{
		source: basepath,
		t:      tmpl,
		e:      e,
	}
	return v, nil
}

func (v *Views) NewView(view string, r *http.Request) *View {
	return &View{
		name: view,
		t:    v.t,
		e:    v.e,
		r:    r,
	}
}

func (v *Views) RenderError(w http.ResponseWriter, r *http.Request, data map[string]interface{}) {
	v.NewView("error", r).Render(w, data)
}

type View struct {
	name string
	t    *template.Template
	e    *Environment
	r    *http.Request
}

func (v *View) Render(w io.Writer, data map[string]interface{}) {
	if data == nil {
		data = make(map[string]interface{})
	}
	data["config"] = v.e.Config
	if err := v.t.ExecuteTemplate(w, v.name, data); err != nil {
		v.e.Log.Errorf("Error rendering template %s", err.Error())
	}
}

var logLevels = map[string]verbose.LogLevel{
	"debug":     verbose.LogLevelDebug,
	"info":      verbose.LogLevelInfo,
	"notice":    verbose.LogLevelNotice,
	"warning":   verbose.LogLevelWarning,
	"error":     verbose.LogLevelError,
	"critical":  verbose.LogLevelCritical,
	"alert":     verbose.LogLevelAlert,
	"emergency": verbose.LogLevelEmergency,
	"fatal":     verbose.LogLevelFatal,
}

type Logger struct {
	*verbose.Logger
	c *Config
}

func NewEmptyLogger() *Logger {
	return &Logger{
		Logger: verbose.New("null"),
	}
}

func NewLogger(c *Config, name string) *Logger {
	logger := verbose.New(name)
	if !c.Logging.Enabled {
		return &Logger{
			Logger: logger,
		}
	}
	sh := verbose.NewStdoutHandler()
	fh, _ := verbose.NewFileHandler(c.Logging.Path)
	logger.AddHandler("stdout", sh)
	logger.AddHandler("file", fh)

	if level, ok := logLevels[c.Logging.Level]; ok {
		sh.SetMinLevel(level)
		fh.SetMinLevel(level)
	}
	// The verbose package sets the default max to Emergancy
	sh.SetMaxLevel(verbose.LogLevelFatal)
	fh.SetMaxLevel(verbose.LogLevelFatal)
	return &Logger{
		Logger: logger,
		c:      c,
	}
}

// GetLogger returns a new Logger based on its parent but with a new name
// This can be used to separate logs from different sub-systems.
func (l *Logger) GetLogger(name string) *Logger {
	return NewLogger(l.c, name)
}
