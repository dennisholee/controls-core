package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// Duration accepts Go duration strings in YAML/JSON.
type Duration struct{ time.Duration }

func (d *Duration) UnmarshalYAML(n *yaml.Node) error {
	var s string
	if err := n.Decode(&s); err != nil {
		return err
	}
	v, err := time.ParseDuration(s)
	if err != nil {
		return fmt.Errorf("duration %q: %w", s, err)
	}
	d.Duration = v
	return nil
}

type sidecarFile struct {
	Server struct {
		Listen       *string `yaml:"listen"`
		MetricsAddr  *string `yaml:"metricsAddr"`
		MaxBodyBytes *int    `yaml:"maxBodyBytes"`
		Debug        *bool   `yaml:"debug"`
	} `yaml:"server"`
	Bundle struct {
		Path           *string   `yaml:"path"`
		PubKey         *string   `yaml:"pubKey"`
		AllowUnsigned  *bool     `yaml:"allowUnsigned"`
		ReloadInterval *Duration `yaml:"reloadInterval"`
	} `yaml:"bundle"`
	PIP struct {
		URL             *string   `yaml:"url"`
		APIVersion      *string   `yaml:"apiVersion"`
		Timeout         *Duration `yaml:"timeout"`
		DefaultTTL      *Duration `yaml:"defaultTTL"`
		MaxTTL          *Duration `yaml:"maxTTL"`
		MaxCache        *int      `yaml:"maxCache"`
		BreakerCooldown *Duration `yaml:"breakerCooldown"`
		TLS             struct {
			CA         *string `yaml:"ca"`
			Cert       *string `yaml:"cert"`
			Key        *string `yaml:"key"`
			ServerName *string `yaml:"serverName"`
		} `yaml:"tls"`
	} `yaml:"pip"`
	Policy struct {
		OPA struct {
			URL     *string   `yaml:"url"`
			Timeout *Duration `yaml:"timeout"`
		} `yaml:"opa"`
	} `yaml:"policy"`
	Identity struct {
		HMACKey *string   `yaml:"hmacKey"`
		MaxSkew *Duration `yaml:"maxSkew"`
	} `yaml:"identity"`
	Audit struct {
		Sample *float64 `yaml:"sample"`
		Buffer *int     `yaml:"buffer"`
		Out    *string  `yaml:"out"`
		Salt   *string  `yaml:"salt"`
	} `yaml:"audit"`
}

func loadSidecarFile(path string) (*sidecarFile, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	dec := yaml.NewDecoder(f)
	dec.KnownFields(true)
	var cfg sidecarFile
	if err := dec.Decode(&cfg); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	var extra any
	if err := dec.Decode(&extra); err != io.EOF {
		return nil, fmt.Errorf("parse %s: multiple documents are not supported", path)
	}
	return &cfg, nil
}

type settings struct {
	Listen          string
	MetricsAddr     string
	MaxBody         int
	Debug           bool
	BundleDir       string
	BundlePubKey    string
	AllowUnsigned   bool
	ReloadInterval  time.Duration
	PIPURL          string
	PIPAPIVersion   string
	PIPTimeout      time.Duration
	PIPDefaultTTL   time.Duration
	PIPMaxTTL       time.Duration
	PIPMaxCache     int
	PIPBreakerCool  time.Duration
	PIPCA           string
	PIPCert         string
	PIPKey          string
	PIPServerName   string
	OPAURL          string
	OPATimeout      time.Duration
	IdentityKey     string
	IdentityMaxSkew time.Duration
	AuditSample     float64
	AuditBuffer     int
	AuditOut        string
	AuditSalt       string
}

func resolveSettings(path string, fs *flag.FlagSet) (settings, error) {
	var f sidecarFile
	if path != "" {
		loaded, err := loadSidecarFile(path)
		if err != nil {
			return settings{}, err
		}
		f = *loaded
		if f.Identity.HMACKey != nil {
			v, err := resolveSecret(*f.Identity.HMACKey)
			if err != nil {
				return settings{}, fmt.Errorf("identity.hmacKey: %w", err)
			}
			f.Identity.HMACKey = &v
		}
		if f.Audit.Salt != nil {
			v, err := resolveSecret(*f.Audit.Salt)
			if err != nil {
				return settings{}, fmt.Errorf("audit.salt: %w", err)
			}
			f.Audit.Salt = &v
		}
	}
	visited := map[string]bool{}
	fs.Visit(func(fl *flag.Flag) { visited[fl.Name] = true })

	pick := func(name, env string, file *string) string {
		if visited[name] {
			return fs.Lookup(name).Value.String()
		}
		if env != "" {
			return env
		}
		if file != nil {
			return *file
		}
		return fs.Lookup(name).Value.String()
	}
	pickInt := func(name string, file *int) int {
		s := ""
		if file != nil {
			s = strconv.Itoa(*file)
		}
		v, _ := strconv.Atoi(pick(name, "", strPtr(s, file != nil)))
		return v
	}
	pickBool := func(name string, file *bool) bool {
		s := ""
		if file != nil {
			s = strconv.FormatBool(*file)
		}
		v, _ := strconv.ParseBool(pick(name, "", strPtr(s, file != nil)))
		return v
	}
	pickFloat := func(name string, file *float64) float64 {
		s := ""
		if file != nil {
			s = strconv.FormatFloat(*file, 'f', -1, 64)
		}
		v, _ := strconv.ParseFloat(pick(name, "", strPtr(s, file != nil)), 64)
		return v
	}
	pickDuration := func(name string, file *Duration) time.Duration {
		s := ""
		if file != nil {
			s = file.Duration.String()
		}
		v, _ := time.ParseDuration(pick(name, "", strPtr(s, file != nil)))
		return v
	}

	s := settings{
		Listen:          pick("listen", "", f.Server.Listen),
		MetricsAddr:     pick("metrics-addr", "", f.Server.MetricsAddr),
		MaxBody:         pickInt("max-body-bytes", f.Server.MaxBodyBytes),
		Debug:           pickBool("debug", f.Server.Debug),
		BundleDir:       pick("config", "", f.Bundle.Path),
		BundlePubKey:    pick("bundle-pubkey", os.Getenv("CONTROLS_BUNDLE_PUBKEY"), f.Bundle.PubKey),
		AllowUnsigned:   pickBool("allow-unsigned", f.Bundle.AllowUnsigned),
		ReloadInterval:  pickDuration("reload-interval", f.Bundle.ReloadInterval),
		PIPURL:          pick("pip", "", f.PIP.URL),
		PIPAPIVersion:   pick("pip-api-version", "", f.PIP.APIVersion),
		PIPTimeout:      pickDuration("pip-timeout", f.PIP.Timeout),
		PIPDefaultTTL:   pickDuration("pip-default-ttl", f.PIP.DefaultTTL),
		PIPMaxTTL:       pickDuration("max-ttl", f.PIP.MaxTTL),
		PIPMaxCache:     pickInt("pip-max-cache", f.PIP.MaxCache),
		PIPBreakerCool:  pickDuration("pip-breaker-cooldown", f.PIP.BreakerCooldown),
		PIPCA:           pick("pip-ca", os.Getenv("CONTROLS_PIP_CA"), f.PIP.TLS.CA),
		PIPCert:         pick("pip-cert", os.Getenv("CONTROLS_PIP_CERT"), f.PIP.TLS.Cert),
		PIPKey:          pick("pip-key", os.Getenv("CONTROLS_PIP_KEY"), f.PIP.TLS.Key),
		PIPServerName:   pick("pip-server-name", os.Getenv("CONTROLS_PIP_SERVER_NAME"), f.PIP.TLS.ServerName),
		OPAURL:          pick("opa-url", os.Getenv("CONTROLS_OPA_URL"), f.Policy.OPA.URL),
		OPATimeout:      pickDuration("opa-timeout", f.Policy.OPA.Timeout),
		IdentityKey:     pick("identity-key", os.Getenv("CONTROLS_IDENTITY_HMAC_KEY"), f.Identity.HMACKey),
		IdentityMaxSkew: pickDuration("identity-max-skew", f.Identity.MaxSkew),
		AuditSample:     pickFloat("audit-sample", f.Audit.Sample),
		AuditBuffer:     pickInt("audit-buffer", f.Audit.Buffer),
		AuditOut:        pick("audit-out", "", f.Audit.Out),
		AuditSalt:       pick("audit-salt", os.Getenv("CONTROLS_AUDIT_SALT"), f.Audit.Salt),
	}
	if s.MaxBody <= 0 {
		return settings{}, errors.New("max body must be positive")
	}
	if s.AuditSample < 0 || s.AuditSample > 1 {
		return settings{}, errors.New("audit sample must be between 0 and 1")
	}
	if s.PIPTimeout <= 0 || s.PIPDefaultTTL <= 0 || s.PIPMaxTTL <= 0 || s.PIPMaxCache <= 0 || s.PIPBreakerCool <= 0 {
		return settings{}, errors.New("pip timeout, TTL, cache and breaker values must be positive")
	}
	if s.OPATimeout <= 0 || s.IdentityMaxSkew <= 0 {
		return settings{}, errors.New("OPA timeout and identity skew must be positive")
	}
	if s.ReloadInterval < 0 {
		return settings{}, errors.New("reload interval cannot be negative")
	}
	return s, nil
}

// resolveSecret supports env:, file: and ${ENV} references. Inline values are
// returned unchanged after environment expansion.
func resolveSecret(v string) (string, error) {
	switch {
	case strings.HasPrefix(v, "env:"):
		name := strings.TrimPrefix(v, "env:")
		val, ok := os.LookupEnv(name)
		if !ok {
			return "", fmt.Errorf("environment variable %q is not set", name)
		}
		return val, nil
	case strings.HasPrefix(v, "file:"):
		raw, err := os.ReadFile(strings.TrimPrefix(v, "file:"))
		if err != nil {
			return "", err
		}
		return strings.TrimSpace(string(raw)), nil
	default:
		return os.ExpandEnv(v), nil
	}
}

func strPtr(v string, ok bool) *string {
	if !ok {
		return nil
	}
	return &v
}
