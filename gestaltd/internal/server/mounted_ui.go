package server

import "net/http"

const browserLoginPath = "/api/v1/auth/login"
const adminUIDirEnv = "GESTALTD_ADMIN_UI_DIR"

type mountedUINavigationPathResolver interface {
	NavigationPathForRequest(string) (string, bool)
}

type protectedUILoginRedirect func(http.ResponseWriter, *http.Request) error
