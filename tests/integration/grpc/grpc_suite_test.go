// Copyright 2018-2021 CERN
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.
//
// In applying this license, CERN does not waive the privileges and immunities
// granted to it by virtue of its status as an Intergovernmental Organization
// or submit itself to any jurisdiction.

package grpc_test

import (
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/mitchellh/mapstructure"
	"github.com/opencloud-eu/reva/v2/pkg/registry"
	"github.com/opencloud-eu/reva/v2/pkg/rgrpc"
	"github.com/opencloud-eu/reva/v2/pkg/rhttp"
	"github.com/opencloud-eu/reva/v2/pkg/sharedconf"
	rtrace "github.com/opencloud-eu/reva/v2/pkg/trace"
	"github.com/pkg/errors"
	"github.com/rs/zerolog"
	"go.opentelemetry.io/otel/trace"
	"go.yaml.in/yaml/v3"

	// These are all the extensions points for REVA
	_ "github.com/opencloud-eu/reva/v2/internal/grpc/interceptors/loader"
	_ "github.com/opencloud-eu/reva/v2/internal/grpc/services/loader"
	_ "github.com/opencloud-eu/reva/v2/internal/http/interceptors/auth/credential/loader"
	_ "github.com/opencloud-eu/reva/v2/internal/http/interceptors/auth/token/loader"
	_ "github.com/opencloud-eu/reva/v2/internal/http/interceptors/auth/tokenwriter/loader"
	_ "github.com/opencloud-eu/reva/v2/internal/http/interceptors/loader"
	_ "github.com/opencloud-eu/reva/v2/internal/http/services/loader"
	_ "github.com/opencloud-eu/reva/v2/pkg/app/provider/loader"
	_ "github.com/opencloud-eu/reva/v2/pkg/app/registry/loader"
	_ "github.com/opencloud-eu/reva/v2/pkg/appauth/manager/loader"
	_ "github.com/opencloud-eu/reva/v2/pkg/auth/manager/loader"
	_ "github.com/opencloud-eu/reva/v2/pkg/auth/registry/loader"
	_ "github.com/opencloud-eu/reva/v2/pkg/cbox/loader"
	_ "github.com/opencloud-eu/reva/v2/pkg/datatx/manager/loader"
	_ "github.com/opencloud-eu/reva/v2/pkg/group/manager/loader"
	_ "github.com/opencloud-eu/reva/v2/pkg/metrics/driver/loader"
	_ "github.com/opencloud-eu/reva/v2/pkg/ocm/invite/repository/loader"
	_ "github.com/opencloud-eu/reva/v2/pkg/ocm/provider/authorizer/loader"
	_ "github.com/opencloud-eu/reva/v2/pkg/ocm/share/repository/loader"
	_ "github.com/opencloud-eu/reva/v2/pkg/permission/manager/loader"
	_ "github.com/opencloud-eu/reva/v2/pkg/preferences/loader"
	_ "github.com/opencloud-eu/reva/v2/pkg/publicshare/manager/loader"
	_ "github.com/opencloud-eu/reva/v2/pkg/rhttp/datatx/manager/loader"
	_ "github.com/opencloud-eu/reva/v2/pkg/share/cache/warmup/loader"
	_ "github.com/opencloud-eu/reva/v2/pkg/share/manager/loader"
	_ "github.com/opencloud-eu/reva/v2/pkg/storage/favorite/loader"
	_ "github.com/opencloud-eu/reva/v2/pkg/storage/fs/loader"
	_ "github.com/opencloud-eu/reva/v2/pkg/storage/registry/loader"
	_ "github.com/opencloud-eu/reva/v2/pkg/token/manager/loader"
	_ "github.com/opencloud-eu/reva/v2/pkg/user/manager/loader"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

const timeoutMs = 30000

var mutex = sync.Mutex{}
var port = 19000

func TestGrpc(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Grpc Suite")
}

type cleanupFunc func(bool) error

// Revad represents a running revad process
type Revad struct {
	TmpRoot     string          // Temporary directory on disk. Will be cleaned up by the Cleanup func.
	StorageRoot string          // Temporary directory used for the revad storage on disk. Will be cleaned up by the Cleanup func.
	GrpcAddress string          // Address of the grpc service
	ID          string          // ID of the grpc service
	Cleanup     cleanupFunc     // Function to kill the process and cleanup the temp. root. If the given parameter is true the files will be kept to make debugging failures easier.
	Process     *InProcessRevad // The running in-process revad
}

type revadCoreConf struct {
	MaxCPUs            string `mapstructure:"max_cpus"`
	TracesExporter     string `mapstructure:"traces_exporter"`
	TracingServiceName string `mapstructure:"tracing_service_name"`

	GracefulShutdownTimeout int `mapstructure:"graceful_shutdown_timeout"`
}

type res struct{}

func (res) isResource() {}

type Resource interface {
	isResource()
}

type Folder struct {
	res
}

type File struct {
	res
	Content  any
	Encoding string // json, plain
}

type RevadConfig struct {
	Name      string
	Config    map[string]any
	Files     map[string]string
	Resources map[string]Resource
}

// startRevads takes a list of revad configuration files plus a map of
// variables that need to be substituted in them and starts them.
//
// A unique port is assigned to each spawned instance.
// Placeholders in the config files can be replaced the variables from the
// `variables` map, e.g. the config
//
//	redis = "{{redis_address}}"
//
// and the variables map
//
//	variables = map[string]string{"redis_address": "localhost:6379"}
//
// will result in the config
//
//	redis = "localhost:6379"
//
// Special variables are created for the revad addresses, e.g. having a
// `storage` and a `users` revad will make `storage_address` and
// `users_address` available wit the dynamically assigned ports so that
// the services can be made available to each other.
func startRevads(configs []RevadConfig, variables map[string]string) (map[string]*Revad, error) {
	mutex.Lock()
	defer mutex.Unlock()

	revads := map[string]*Revad{}
	addresses := map[string]string{}
	ids := map[string]string{}
	roots := map[string]string{}

	tmpBase, err := os.MkdirTemp("", "reva-grpc-integration-tests")
	if err != nil {
		return nil, errors.Wrapf(err, "Could not create tmpdir")
	}

	for _, c := range configs {
		ids[c.Name] = uuid.New().String()
		// Create a temporary root for this revad
		tmpRoot := path.Join(tmpBase, c.Name)
		roots[c.Name] = tmpRoot
		addresses[c.Name] = fmt.Sprintf("localhost:%d", port)
		port++
		addresses[c.Name+"+1"] = fmt.Sprintf("localhost:%d", port)
		port++
		addresses[c.Name+"+2"] = fmt.Sprintf("localhost:%d", port)
		port++
	}

	for _, c := range configs {
		ownAddress := addresses[c.Name]
		ownID := ids[c.Name]
		filesPath := map[string]string{}

		tmpRoot := roots[c.Name]
		err := os.Mkdir(tmpRoot, 0755)
		if err != nil {
			return nil, errors.Wrapf(err, "Could not create tmpdir")
		}

		// newCfgPath := path.Join(tmpRoot, "config.toml")
		// rawCfg, err := os.ReadFile(path.Join("fixtures", c.Config))
		// if err != nil {
		// 	return nil, errors.Wrapf(err, "Could not read config file")
		// }

		for name, p := range c.Files {
			rawFile, err := os.ReadFile(path.Join("fixtures", p))
			if err != nil {
				return nil, errors.Wrapf(err, "error reading file")
			}
			cfg := string(rawFile)
			for v, value := range variables {
				cfg = strings.ReplaceAll(cfg, "{{"+v+"}}", value)
			}
			for name, address := range addresses {
				cfg = strings.ReplaceAll(cfg, "{{"+name+"_address}}", address)
			}
			newFilePath := path.Join(tmpRoot, p)
			err = os.WriteFile(newFilePath, []byte(cfg), 0600)
			if err != nil {
				return nil, errors.Wrapf(err, "error writing file")
			}
			filesPath[name] = newFilePath
		}
		for name, resource := range c.Resources {
			tmpResourcePath := filepath.Join(tmpRoot, name)

			switch r := resource.(type) {
			case File:
				// fill the file with the initial content
				switch r.Encoding {
				case "", "plain":
					if err := os.WriteFile(tmpResourcePath, []byte(r.Content.(string)), 0644); err != nil {
						return nil, err
					}
				case "json":
					d, err := json.Marshal(r.Content)
					if err != nil {
						return nil, err
					}
					if err := os.WriteFile(tmpResourcePath, d, 0644); err != nil {
						return nil, err
					}
				default:
					return nil, errors.New("encoding not known " + r.Encoding)
				}
			case Folder:
				if err := os.MkdirAll(tmpResourcePath, 0755); err != nil {
					return nil, err
				}
			}

			filesPath[name] = tmpResourcePath
		}

		replacements := map[string]string{
			"{{root}}":           tmpRoot,
			"{{id}}":             ownID,
			"{{grpc_address}}":   ownAddress,
			"{{grpc_address+1}}": addresses[c.Name+"+1"],
			"{{grpc_address+2}}": addresses[c.Name+"+2"],
		}

		for name, path := range filesPath {
			replacements["{{file_"+name+"}}"] = path
			replacements["{{"+name+"}}"] = path
		}
		for v, value := range variables {
			replacements["{{"+v+"}}"] = value
		}
		for name, address := range addresses {
			replacements["{{"+name+"_address}}"] = address
		}
		for name, id := range ids {
			replacements["{{"+name+"_id}}"] = id
		}
		for name, root := range roots {
			replacements["{{"+name+"_root}}"] = root
		}

		cfgMap := copyConfig(c.Config)
		replacePlaceholders(cfgMap, replacements)

		ymlConfig, err := yaml.Marshal(cfgMap)
		if err != nil {
			return nil, errors.Wrapf(err, "Could not marshal config to YAML")
		}
		err = os.WriteFile(filepath.Join(tmpRoot, "config.yml"), ymlConfig, 0600)
		if err != nil {
			return nil, errors.Wrapf(err, "Could not write config file")
		}

		proc, err := startRevaServicesInProcess(cfgMap, tmpRoot, "debug")
		if err != nil {
			return nil, errors.Wrapf(err, "Could not start revad")
		}

		err = waitForPort(ownAddress, "open")
		if err != nil {
			return nil, err
		}

		// even the port is open the service might not be available yet
		time.Sleep(2 * time.Second)

		revad := &Revad{
			TmpRoot:     tmpRoot,
			StorageRoot: path.Join(tmpRoot, "storage"),
			GrpcAddress: ownAddress,
			ID:          ownID,
			Process:     proc,
			Cleanup: func(keepLogs bool) error {
				proc.Stop()

				if keepLogs {
					fmt.Println("Test failed, keeping root", tmpRoot, "around for debugging")
				} else {
					os.RemoveAll(tmpRoot)
					os.Remove(tmpBase) // Remove base temp dir if it's empty
				}
				return nil
			},
		}
		revads[c.Name] = revad
	}
	return revads, nil
}

func isEnabled(key string, conf map[string]any) bool {
	if a, ok := conf[key]; ok {
		if b, ok := a.(map[string]any); ok {
			if c, ok := b["services"]; ok {
				if d, ok := c.(map[string]any); ok {
					if len(d) > 0 {
						return true
					}
				}
			}
		}
	}
	return false
}

type Server interface {
	Network() string
	Address() string
	GracefulStop() error
	Stop() error
}

type InProcessRevad struct {
	Servers   map[string]Server
	Listeners map[string]net.Listener
}

func (r *InProcessRevad) Stop() {
	for _, s := range r.Servers {
		_ = s.Stop()
	}
	for _, l := range r.Listeners {
		_ = l.Close()
	}
}

func startRevaServicesInProcess(mainConf map[string]any, tmpRoot, logLevel string) (*InProcessRevad, error) {
	// reset shared config to not pollute config between tests. This is needed as we are running the revads in-process and the shared config is global.
	sharedconf.ResetOnce()

	if err := sharedconf.Decode(mainConf["shared"]); err != nil {
		return nil, err
	}

	coreConf := &revadCoreConf{}
	if err := mapstructure.Decode(mainConf["core"], coreConf); err != nil {
		return nil, err
	}

	if coreConf.TracesExporter == "" {
		coreConf.TracesExporter = "console"
	}

	logFile, err := os.Create(filepath.Join(tmpRoot, "revad.log"))
	if err != nil {
		return nil, err
	}
	zerolog.SetGlobalLevel(zerolog.TraceLevel)
	log := zerolog.New(logFile)
	log = log.Level(zerolog.DebugLevel)
	if logLevel != "" {
		lvl, err := zerolog.ParseLevel(logLevel)
		if err == nil {
			log = log.Level(lvl)
		}
	}

	s, _ := json.MarshalIndent(mainConf, "", "	")
	log.Info().Msg("Starting revad with config: " + string(s))

	// We pass nil options to registry.Init as default
	if err := registry.Init(nil); err != nil {
		return nil, err
	}

	// init tracer
	var tp trace.TracerProvider
	if coreConf.TracesExporter != "none" && coreConf.TracesExporter != "" {
		tp = rtrace.NewTracerProvider(coreConf.TracingServiceName, coreConf.TracesExporter)
		rtrace.SetDefaultTracerProvider(tp)
	} else {
		tp = rtrace.DefaultProvider()
	}

	// init servers
	servers := map[string]Server{}
	if isEnabled("http", mainConf) {
		sublog := log.With().Str("pkg", "rhttp").Logger()
		s, err := rhttp.New(mainConf["http"], sublog, tp)
		if err != nil {
			return nil, err
		}
		servers["http"] = s
	}

	if isEnabled("grpc", mainConf) {
		sublog := log.With().Str("pkg", "rgrpc").Logger()
		s, err := rgrpc.NewServer(mainConf["grpc"], sublog, tp)
		if err != nil {
			return nil, err
		}
		servers["grpc"] = s
	}

	// init listeners
	listeners := map[string]net.Listener{}
	for k, s := range servers {
		ln, err := net.Listen(s.Network(), s.Address())
		if err != nil {
			return nil, err
		}
		listeners[k] = ln
	}

	// start servers
	if s, ok := servers["http"]; ok {
		go func() {
			if err := s.(*rhttp.Server).Start(listeners["http"]); err != nil {
				log.Error().Err(err).Msg("error starting the http server")
			}
		}()
	}
	if s, ok := servers["grpc"]; ok {
		go func() {
			if err := s.(*rgrpc.Server).Start(listeners["grpc"]); err != nil {
				log.Error().Err(err).Msg("error starting the grpc server")
			}
		}()
	}

	return &InProcessRevad{
		Servers:   servers,
		Listeners: listeners,
	}, nil
}

func waitForPort(grpcAddress, expectedStatus string) error {
	if expectedStatus != "open" && expectedStatus != "close" {
		return errors.New("status can only be 'open' or 'close'")
	}
	timoutCounter := 0
	for timoutCounter <= timeoutMs {
		conn, err := net.Dial("tcp", grpcAddress)
		if err == nil {
			_ = conn.Close()
			if expectedStatus == "open" {
				break
			}
		} else if expectedStatus == "close" {
			break
		}

		time.Sleep(1 * time.Millisecond)
		timoutCounter++
	}
	return nil
}

func copyConfig(m map[string]any) map[string]any {
	cp := make(map[string]any)
	for k, v := range m {
		if vm, ok := v.(map[string]any); ok {
			cp[k] = copyConfig(vm)
		} else {
			cp[k] = v
		}
	}
	return cp
}

func replacePlaceholders(m map[string]any, replacements map[string]string) {
	for k, v := range m {
		// replace keys
		for place, val := range replacements {
			newKey := strings.ReplaceAll(k, place, val)
			if newKey != k {
				delete(m, k)
				m[newKey] = v
				k = newKey
			}
		}

		// replace values
		if vm, ok := v.(map[string]any); ok {
			replacePlaceholders(vm, replacements)
		} else if s, ok := v.(string); ok {
			for place, val := range replacements {
				s = strings.ReplaceAll(s, place, val)
			}
			m[k] = s
		}
	}
}
