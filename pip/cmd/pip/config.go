package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strconv"
	"time"

	"gopkg.in/yaml.v3"
)

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

type pipFile struct {
	Server struct {
		Addr          *string `yaml:"addr"`
		MetricsAddr   *string `yaml:"metricsAddr"`
		MaxConcurrent *int    `yaml:"maxConcurrent"`
		RequireCaller *bool   `yaml:"requireCaller"`
		RequireMTLS   *bool   `yaml:"requireMTLS"`
		RateLimit     *int    `yaml:"rateLimit"`
		RateBurst     *int    `yaml:"rateBurst"`
		TLSCert       *string `yaml:"tlsCert"`
		TLSKey        *string `yaml:"tlsKey"`
		ClientCA      *string `yaml:"clientCA"`
		LagSeconds    *int    `yaml:"lagSeconds"`
	} `yaml:"server"`
	Repository struct {
		Fixtures *string `yaml:"fixtures"`
		Sources  *string `yaml:"sources"`
	} `yaml:"repository"`
}

func loadPIPFile(path string) (*pipFile, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	dec := yaml.NewDecoder(f)
	dec.KnownFields(true)
	var cfg pipFile
	if err := dec.Decode(&cfg); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	var extra any
	if err := dec.Decode(&extra); err != io.EOF {
		return nil, fmt.Errorf("parse %s: multiple documents are not supported", path)
	}
	return &cfg, nil
}

type pipSettings struct {
	Addr          string
	MetricsAddr   string
	MaxConcurrent int
	RequireCaller bool
	RequireMTLS   bool
	RateLimit     int
	RateBurst     int
	TLSCert       string
	TLSKey        string
	ClientCA      string
	LagSeconds    int
	Fixtures      string
	Sources       string
}

func resolvePIPSettings(path string, fs *flag.FlagSet) (pipSettings, error) {
	var f pipFile
	if path != "" {
		loaded, err := loadPIPFile(path)
		if err != nil {
			return pipSettings{}, err
		}
		f = *loaded
	}
	visited := map[string]bool{}
	fs.Visit(func(fl *flag.Flag) { visited[fl.Name] = true })
	pick := func(name string, file *string) string {
		if visited[name] {
			return fs.Lookup(name).Value.String()
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
		v, _ := strconv.Atoi(pick(name, strPtr(s, file != nil)))
		return v
	}
	pickBool := func(name string, file *bool) bool {
		s := ""
		if file != nil {
			s = strconv.FormatBool(*file)
		}
		v, _ := strconv.ParseBool(pick(name, strPtr(s, file != nil)))
		return v
	}
	s := pipSettings{
		Addr:          pick("addr", f.Server.Addr),
		MetricsAddr:   pick("metrics-addr", f.Server.MetricsAddr),
		MaxConcurrent: pickInt("max-concurrent", f.Server.MaxConcurrent),
		RequireCaller: pickBool("require-caller", f.Server.RequireCaller),
		RequireMTLS:   pickBool("require-mtls", f.Server.RequireMTLS),
		RateLimit:     pickInt("rate-limit", f.Server.RateLimit),
		RateBurst:     pickInt("rate-burst", f.Server.RateBurst),
		TLSCert:       pick("tls-cert", f.Server.TLSCert),
		TLSKey:        pick("tls-key", f.Server.TLSKey),
		ClientCA:      pick("client-ca", f.Server.ClientCA),
		LagSeconds:    pickInt("lag-seconds", f.Server.LagSeconds),
		Fixtures:      pick("fixtures", f.Repository.Fixtures),
		Sources:       pick("sources", f.Repository.Sources),
	}
	if s.MaxConcurrent < 0 || s.RateLimit < 0 || s.RateBurst < 0 || s.LagSeconds < 0 {
		return pipSettings{}, errors.New("concurrency, rate and lag values cannot be negative")
	}
	if (s.TLSCert == "") != (s.TLSKey == "") {
		return pipSettings{}, errors.New("tlsCert and tlsKey must be set together")
	}
	if s.RequireMTLS && (s.TLSCert == "" || s.TLSKey == "" || s.ClientCA == "") {
		return pipSettings{}, errors.New("requireMTLS needs tlsCert, tlsKey and clientCA")
	}
	if s.ClientCA != "" && s.TLSCert == "" {
		return pipSettings{}, errors.New("clientCA requires tlsCert and tlsKey")
	}
	return s, nil
}

func strPtr(v string, ok bool) *string {
	if !ok {
		return nil
	}
	return &v
}
