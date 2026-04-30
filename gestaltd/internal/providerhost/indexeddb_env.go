package providerhost

import indexeddbservice "github.com/valon-technologies/gestalt/server/services/indexeddb"

const DefaultIndexedDBSocketEnv = indexeddbservice.DefaultSocketEnv

func IndexedDBSocketEnv(name string) string {
	return indexeddbservice.SocketEnv(name)
}

func IndexedDBSocketTokenEnv(name string) string {
	return indexeddbservice.SocketTokenEnv(name)
}
