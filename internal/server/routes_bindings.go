package server

import (
	"log"

	"github.com/go-chi/chi/v5"
)

func (s *Server) mountBindingRoutes(r chi.Router) {
	if s.bindings == nil {
		return
	}
	for _, name := range s.bindings.List() {
		binding, err := s.bindings.Get(name)
		if err != nil {
			log.Printf("warning: skipping binding %q routes: %v", name, err)
			continue
		}
		for _, route := range binding.Routes() {
			handler := route.Handler
			if !route.Public {
				if route.ProxyAuth {
					handler = s.proxyAuthMiddleware(handler)
				} else {
					handler = s.authMiddleware(handler)
				}
			}
			r.Method(route.Method, "/bindings/"+name+route.Pattern, handler)
		}
	}
}
