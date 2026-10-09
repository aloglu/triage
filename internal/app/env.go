// Package app holds the environment shared by triage's command line and
// interactive app.
package app

import (
	"errors"
	"fmt"
	"io"
	"os"
	"runtime/debug"
	"strings"

	"github.com/cli/go-gh/v2/pkg/browser"
	"github.com/cli/go-gh/v2/pkg/repository"

	"github.com/aloglu/triage/internal/config"
	"github.com/aloglu/triage/internal/engine"
	"github.com/aloglu/triage/internal/gh"
	"github.com/aloglu/triage/internal/store"
)

// Env is everything a command needs; tests substitute their own.
type Env struct {
	Paths        config.Paths
	Config       config.Config
	ConfigExists bool
	// ConfigErr is a problem found while loading config.toml.
	ConfigErr error

	Client *gh.Client
	// ClientErr explains why Client is nil.
	ClientErr error
	Engine    *engine.Engine

	In       io.Reader
	Out, Err io.Writer
	// IsTerminal reports whether stdout is a terminal.
	IsTerminal bool

	// CurrentRepo returns the GitHub repo of the working directory.
	CurrentRepo func() (string, error)
	// OpenURL opens a URL in the browser.
	OpenURL func(string) error
	// Version is the build version.
	Version string
}

// NewEnv builds the real environment. It never fails outright: missing auth
// or a broken config are recorded so commands can explain them.
func NewEnv(version string) (*Env, error) {
	paths, err := config.DefaultPaths()
	if err != nil {
		return nil, err
	}
	env := &Env{
		Paths:      paths,
		In:         os.Stdin,
		Out:        os.Stdout,
		Err:        os.Stderr,
		IsTerminal: isTerminal(os.Stdout),
		Version:    ResolveVersion(version),
	}
	env.Config, env.ConfigExists, env.ConfigErr = config.Load(paths.ConfigFile())
	env.Client, env.ClientErr = gh.New()
	env.CurrentRepo = func() (string, error) {
		repo, err := repository.Current()
		if err != nil {
			return "", err
		}
		if env.Client != nil && !strings.EqualFold(repo.Host, env.Client.Host()) {
			return "", fmt.Errorf("current repository is on %s, not %s", repo.Host, env.Client.Host())
		}
		return repo.Owner + "/" + repo.Name, nil
	}
	env.OpenURL = func(url string) error {
		return browser.New("", env.Out, env.Err).Browse(url)
	}
	env.initEngine()
	return env, nil
}

func (env *Env) initEngine() {
	env.Engine = engine.New(env.Client, store.New(env.Paths), env.Config.Labels)
}

// NewTestEnv builds an environment around a fake GitHub client.
func NewTestEnv(paths config.Paths, client *gh.Client, out, errOut io.Writer) *Env {
	cfg, exists, cfgErr := config.Load(paths.ConfigFile())
	env := &Env{
		Paths: paths, Config: cfg, ConfigExists: exists, ConfigErr: cfgErr,
		Client: client, In: strings.NewReader(""), Out: out, Err: errOut,
		CurrentRepo: func() (string, error) { return "", errors.New("not a git repository") },
		OpenURL:     func(string) error { return nil },
		Version:     "test",
	}
	env.initEngine()
	return env
}

// SetClient replaces the GitHub client, e.g. after the user signs in.
func (env *Env) SetClient(client *gh.Client) {
	env.Client, env.ClientErr = client, nil
	env.initEngine()
}

// SaveConfig writes the config and rebuilds the engine around it.
func (env *Env) SaveConfig() error {
	if err := config.Save(env.Paths.ConfigFile(), env.Config); err != nil {
		return err
	}
	env.ConfigExists = true
	env.ConfigErr = nil
	env.initEngine()
	return nil
}

// RequireClient returns a friendly error when GitHub isn't reachable.
func (env *Env) RequireClient() error {
	if env.Client != nil {
		return nil
	}
	if env.ClientErr != nil {
		return errors.New(gh.UserMessage(env.ClientErr))
	}
	return errors.New("not logged in to GitHub; run `gh auth login`")
}

// ResolveVersion returns version, or the module version recorded by
// `go install` when version wasn't set at build time.
func ResolveVersion(version string) string {
	if version != "" && version != "dev" {
		return version
	}
	if info, ok := debug.ReadBuildInfo(); ok && info.Main.Version != "" && info.Main.Version != "(devel)" {
		return info.Main.Version
	}
	return "dev"
}

func isTerminal(f *os.File) bool {
	info, err := f.Stat()
	return err == nil && info.Mode()&os.ModeCharDevice != 0
}
