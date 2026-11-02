package hub

import "net/http"

// peerRoutes are the mesh's routes on the API: the link and pairing between hubs.
func (h *Hub) peerRoutes(api *http.ServeMux) {
	api.HandleFunc("GET "+peerPath, h.servePeer)
	api.HandleFunc("POST "+peerPairPath, h.servePeerPair)
}
