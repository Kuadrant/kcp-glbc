/*
Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package metrics

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"strconv"
	"time"

	"github.com/prometheus/client_golang/prometheus/promhttp"

	"k8s.io/klog/v2"
)

const defaultMetricsEndpoint = "/metrics"

type Server struct {
	httpServer http.Server
	listener   net.Listener
}

func NewServer(port *int) (*Server, error) {
	addr := "0"
	if port != nil && *port != 0 {
		addr = ":" + strconv.Itoa(*port)
	}

	listener, err := newListener(addr)
	if err != nil {
		return nil, err
	}

	handler := promhttp.HandlerFor(Registry, promhttp.HandlerOpts{
		ErrorHandling: promhttp.HTTPErrorOnError,
	})
	mux := http.NewServeMux()
	mux.Handle(defaultMetricsEndpoint, handler)

	return &Server{
		listener: listener,
		httpServer: http.Server{
			Handler: mux,
		},
	}, nil
}

func (s *Server) Start() (err error) {
	if s.listener == nil {
		klog.InfoS("Serving metrics is disabled")
		return
	}
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("serving metrics failed: %v", r)
		}
	}()
	klog.InfoS("Started serving metrics", "address", s.listener.Addr())
	if e := s.httpServer.Serve(s.listener); e != http.ErrServerClosed {
		err = e
	}
	return
}

func (s *Server) Shutdown() error {
	if s.listener == nil {
		return nil
	}
	klog.Info("Stopping metrics server")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	return s.httpServer.Shutdown(shutdownCtx)
}

// newListener creates a new TCP listener bound to the given address.
func newListener(addr string) (net.Listener, error) {
	// Add a case to disable metrics altogether
	if addr == "0" {
		return nil, nil
	}

	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return nil, err
	}

	return listener, nil
}
