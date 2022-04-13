package pprof

import (
	"net/http"
	"time"

	"github.com/rs/zerolog/log"

	_ "net/http/pprof"
)

// Server holds pprof server required informations
type Server struct {
	http.Server
}

// New returns a pprof server instance
func New(addr string) (*Server, error) {
	s := Server{}
	s.ReadTimeout = time.Second
	s.Addr = addr
	return &s, nil
}

// Run the pprof server
func (p *Server) Run() {
	if err := p.ListenAndServe(); err != nil {
		log.Fatal().Err(err).Msg("error running pprof server")
	}
}
