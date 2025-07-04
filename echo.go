// SPDX-License-Identifier: MIT
// SPDX-FileCopyrightText: © 2015 LabStack LLC and Echo contributors

/*
Package echo implements high performance, minimalist Go web framework.

Example:

	package main

	import (
	  "net/http"

	  "github.com/labstack/echo/v4"
	  "github.com/labstack/echo/v4/middleware"
	)

	// Handler
	func hello(c echo.Context) error {
	  return c.String(http.StatusOK, "Hello, World!")
	}

	func main() {
	  // Echo instance
	  e := echo.New()

	  // Middleware
	  e.Use(middleware.Logger())
	  e.Use(middleware.Recover())

	  // Routes
	  e.GET("/", hello)

	  // Start server
	  e.Logger.Fatal(e.Start(":1323"))
	}

Learn more at https://echo.labstack.com
*/
package echo

import (
	stdContext "context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	stdLog "log"
	"net"
	"net/http"
	"os"
	"reflect"
	"runtime"
	"sync"
	"time"

	//"github.com/labstack/gommon/color"
	//"github.com/labstack/gommon/log"
	"golang.org/x/crypto/acme"
	"golang.org/x/crypto/acme/autocert"
	"golang.org/x/net/http2"
	"golang.org/x/net/http2/h2c"
)

// Echo is the top-level framework instance.
//
// Goroutine safety: Do not mutate Echo instance fields after server has started. Accessing these
// fields from handlers/middlewares and changing field values at the same time leads to data-races.
// Adding new routes after the server has been started is also not safe!
type Echo struct {
	filesystem
	common
	// startupMutex is mutex to lock Echo instance access during server configuration and startup. Useful for to get
	// listener address info (on which interface/port was listener bound) without having data races.
	startupMutex sync.RWMutex
	//colorer      *color.Color

	// premiddleware are middlewares that are run before routing is done. In case a pre-middleware returns
	// an error the router is not executed and the request will end up in the global error handler.
	premiddleware []MiddlewareFunc
	middleware    []MiddlewareFunc
	maxParam      *int
	router        *Router
	routers       map[string]*Router
	pool          sync.Pool

	StdLogger        *stdLog.Logger
	Server           *http.Server
	TLSServer        *http.Server
	Listener         net.Listener
	TLSListener      net.Listener
	AutoTLSManager   autocert.Manager
	HTTPErrorHandler HTTPErrorHandler
	Binder           Binder
	JSONSerializer   JSONSerializer
	Validator        Validator
	Renderer         Renderer
	Logger           Logger
	IPExtractor      IPExtractor
	ListenerNetwork  string

	// OnAddRouteHandler is called when Echo adds new route to specific host router.
	OnAddRouteHandler func(host string, route Route, handler HandlerFunc, middleware []MiddlewareFunc)
	DisableHTTP2      bool
	Debug             bool
	HideBanner        bool
	HidePort          bool
}

// Route contains a handler and information for matching against requests.
type Route struct {
	Method string `json:"method"`
	Path   string `json:"path"`
	Name   string `json:"name"`
}

// HTTPError represents an error that occurred while handling a request.
type HTTPError struct {
	Internal error       `json:"-"` // Stores the error returned by an external dependency
	Message  interface{} `json:"message"`
	Code     int         `json:"-"`
}

// MiddlewareFunc defines a function to process middleware.
type MiddlewareFunc func(next HandlerFunc) HandlerFunc

// HandlerFunc defines a function to serve HTTP requests.
type HandlerFunc func(c Context) error

// HTTPErrorHandler is a centralized HTTP error handler.
type HTTPErrorHandler func(err error, c Context)

// Validator is the interface that wraps the Validate function.
type Validator interface {
	Validate(i interface{}) error
}

// JSONSerializer is the interface that encodes and decodes JSON to and from interfaces.
type JSONSerializer interface {
	Serialize(c Context, i interface{}, indent string) error
	Deserialize(c Context, i interface{}) error
}

// Map defines a generic map of type `map[string]interface{}`.
type Map map[string]interface{}

// Common struct for Echo & Group.
type common struct{}

// HTTP methods
// NOTE: Deprecated, please use the stdlib constants directly instead.
const (
	CONNECT = http.MethodConnect
	DELETE  = http.MethodDelete
	GET     = http.MethodGet
	HEAD    = http.MethodHead
	OPTIONS = http.MethodOptions
	PATCH   = http.MethodPatch
	POST    = http.MethodPost
	// PROPFIND = "PROPFIND"
	PUT   = http.MethodPut
	TRACE = http.MethodTrace
)

// MIME types
const (
	// MIMEApplicationJSON JavaScript Object Notation (JSON) https://www.rfc-editor.org/rfc/rfc8259
	MIMEApplicationJSON = "application/json"
	// Deprecated: Please use MIMEApplicationJSON instead. JSON should be encoded using UTF-8 by default.
	// No "charset" parameter is defined for this registration.
	// Adding one really has no effect on compliant recipients.
	// See RFC 8259, section 8.1. https://datatracker.ietf.org/doc/html/rfc8259#section-8.1
	MIMEApplicationJSONCharsetUTF8       = MIMEApplicationJSON + "; " + charsetUTF8
	MIMEApplicationJavaScript            = "application/javascript"
	MIMEApplicationJavaScriptCharsetUTF8 = MIMEApplicationJavaScript + "; " + charsetUTF8
	MIMEApplicationXML                   = "application/xml"
	MIMEApplicationXMLCharsetUTF8        = MIMEApplicationXML + "; " + charsetUTF8
	MIMETextXML                          = "text/xml"
	MIMETextXMLCharsetUTF8               = MIMETextXML + "; " + charsetUTF8
	MIMEApplicationForm                  = "application/x-www-form-urlencoded"
	MIMEApplicationProtobuf              = "application/protobuf"
	MIMEApplicationMsgpack               = "application/msgpack"
	MIMETextHTML                         = "text/html"
	MIMETextHTMLCharsetUTF8              = MIMETextHTML + "; " + charsetUTF8
	MIMETextPlain                        = "text/plain"
	MIMETextPlainCharsetUTF8             = MIMETextPlain + "; " + charsetUTF8
	MIMEMultipartForm                    = "multipart/form-data"
	MIMEOctetStream                      = "application/octet-stream"
)

const (
	charsetUTF8 = "charset=UTF-8"
	// PROPFIND Method can be used on collection and property resources.
	PROPFIND = "PROPFIND"
	// REPORT Method can be used to get information about a resource, see rfc 3253
	REPORT = "REPORT"
	// RouteNotFound is special method type for routes handling "route not found" (404) cases
	RouteNotFound = "echo_route_not_found"
)

// Headers
const (
	HeaderAccept         = "Accept"
	HeaderAcceptEncoding = "Accept-Encoding"
	// HeaderAllow is the name of the "Allow" header field used to list the set of methods
	// advertised as supported by the target resource. Returning an Allow header is mandatory
	// for status 405 (method not found) and useful for the OPTIONS method in responses.
	// See RFC 7231: https://datatracker.ietf.org/doc/html/rfc7231#section-7.4.1
	HeaderAllow               = "Allow"
	HeaderAuthorization       = "Authorization"
	HeaderContentDisposition  = "Content-Disposition"
	HeaderContentEncoding     = "Content-Encoding"
	HeaderContentLength       = "Content-Length"
	HeaderContentType         = "Content-Type"
	HeaderCookie              = "Cookie"
	HeaderSetCookie           = "Set-Cookie"
	HeaderIfModifiedSince     = "If-Modified-Since"
	HeaderLastModified        = "Last-Modified"
	HeaderLocation            = "Location"
	HeaderRetryAfter          = "Retry-After"
	HeaderUpgrade             = "Upgrade"
	HeaderVary                = "Vary"
	HeaderWWWAuthenticate     = "WWW-Authenticate"
	HeaderXForwardedFor       = "X-Forwarded-For"
	HeaderXForwardedProto     = "X-Forwarded-Proto"
	HeaderXForwardedProtocol  = "X-Forwarded-Protocol"
	HeaderXForwardedSsl       = "X-Forwarded-Ssl"
	HeaderXUrlScheme          = "X-Url-Scheme"
	HeaderXHTTPMethodOverride = "X-HTTP-Method-Override"
	HeaderXRealIP             = "X-Real-Ip"
	HeaderXRequestID          = "X-Request-Id"
	HeaderXCorrelationID      = "X-Correlation-Id"
	HeaderXRequestedWith      = "X-Requested-With"
	HeaderServer              = "Server"
	HeaderOrigin              = "Origin"
	HeaderCacheControl        = "Cache-Control"
	HeaderConnection          = "Connection"

	// Access control
	HeaderAccessControlRequestMethod    = "Access-Control-Request-Method"
	HeaderAccessControlRequestHeaders   = "Access-Control-Request-Headers"
	HeaderAccessControlAllowOrigin      = "Access-Control-Allow-Origin"
	HeaderAccessControlAllowMethods     = "Access-Control-Allow-Methods"
	HeaderAccessControlAllowHeaders     = "Access-Control-Allow-Headers"
	HeaderAccessControlAllowCredentials = "Access-Control-Allow-Credentials"
	HeaderAccessControlExposeHeaders    = "Access-Control-Expose-Headers"
	HeaderAccessControlMaxAge           = "Access-Control-Max-Age"

	// Security
	HeaderStrictTransportSecurity         = "Strict-Transport-Security"
	HeaderXContentTypeOptions             = "X-Content-Type-Options"
	HeaderXXSSProtection                  = "X-XSS-Protection"
	HeaderXFrameOptions                   = "X-Frame-Options"
	HeaderContentSecurityPolicy           = "Content-Security-Policy"
	HeaderContentSecurityPolicyReportOnly = "Content-Security-Policy-Report-Only"
	HeaderXCSRFToken                      = "X-CSRF-Token"
	HeaderReferrerPolicy                  = "Referrer-Policy"
)

const (
	// Version of Echo
	Version = "4.13.4"
	website = "https://echo.labstack.com"
	// http://patorjk.com/software/taag/#p=display&f=Small%20Slant&t=Echo
	banner = `
   ____    __
  / __/___/ /  ___
 / _// __/ _ \/ _ \
/___/\__/_//_/\___/ %s
High performance, minimalist Go web framework
%s
____________________________________O/_______
                                    O\
`
)

var methods = [...]string{
	http.MethodConnect,
	http.MethodDelete,
	http.MethodGet,
	http.MethodHead,
	http.MethodOptions,
	http.MethodPatch,
	http.MethodPost,
	PROPFIND,
	http.MethodPut,
	http.MethodTrace,
	REPORT,
}

// Errors
var (
	ErrBadRequest                    = NewHTTPError(http.StatusBadRequest)                    // HTTP 400 Bad Request
	ErrUnauthorized                  = NewHTTPError(http.StatusUnauthorized)                  // HTTP 401 Unauthorized
	ErrPaymentRequired               = NewHTTPError(http.StatusPaymentRequired)               // HTTP 402 Payment Required
	ErrForbidden                     = NewHTTPError(http.StatusForbidden)                     // HTTP 403 Forbidden
	ErrNotFound                      = NewHTTPError(http.StatusNotFound)                      // HTTP 404 Not Found
	ErrMethodNotAllowed              = NewHTTPError(http.StatusMethodNotAllowed)              // HTTP 405 Method Not Allowed
	ErrNotAcceptable                 = NewHTTPError(http.StatusNotAcceptable)                 // HTTP 406 Not Acceptable
	ErrProxyAuthRequired             = NewHTTPError(http.StatusProxyAuthRequired)             // HTTP 407 Proxy AuthRequired
	ErrRequestTimeout                = NewHTTPError(http.StatusRequestTimeout)                // HTTP 408 Request Timeout
	ErrConflict                      = NewHTTPError(http.StatusConflict)                      // HTTP 409 Conflict
	ErrGone                          = NewHTTPError(http.StatusGone)                          // HTTP 410 Gone
	ErrLengthRequired                = NewHTTPError(http.StatusLengthRequired)                // HTTP 411 Length Required
	ErrPreconditionFailed            = NewHTTPError(http.StatusPreconditionFailed)            // HTTP 412 Precondition Failed
	ErrStatusRequestEntityTooLarge   = NewHTTPError(http.StatusRequestEntityTooLarge)         // HTTP 413 Payload Too Large
	ErrRequestURITooLong             = NewHTTPError(http.StatusRequestURITooLong)             // HTTP 414 URI Too Long
	ErrUnsupportedMediaType          = NewHTTPError(http.StatusUnsupportedMediaType)          // HTTP 415 Unsupported Media Type
	ErrRequestedRangeNotSatisfiable  = NewHTTPError(http.StatusRequestedRangeNotSatisfiable)  // HTTP 416 Range Not Satisfiable
	ErrExpectationFailed             = NewHTTPError(http.StatusExpectationFailed)             // HTTP 417 Expectation Failed
	ErrTeapot                        = NewHTTPError(http.StatusTeapot)                        // HTTP 418 I'm a teapot
	ErrMisdirectedRequest            = NewHTTPError(http.StatusMisdirectedRequest)            // HTTP 421 Misdirected Request
	ErrUnprocessableEntity           = NewHTTPError(http.StatusUnprocessableEntity)           // HTTP 422 Unprocessable Entity
	ErrLocked                        = NewHTTPError(http.StatusLocked)                        // HTTP 423 Locked
	ErrFailedDependency              = NewHTTPError(http.StatusFailedDependency)              // HTTP 424 Failed Dependency
	ErrTooEarly                      = NewHTTPError(http.StatusTooEarly)                      // HTTP 425 Too Early
	ErrUpgradeRequired               = NewHTTPError(http.StatusUpgradeRequired)               // HTTP 426 Upgrade Required
	ErrPreconditionRequired          = NewHTTPError(http.StatusPreconditionRequired)          // HTTP 428 Precondition Required
	ErrTooManyRequests               = NewHTTPError(http.StatusTooManyRequests)               // HTTP 429 Too Many Requests
	ErrRequestHeaderFieldsTooLarge   = NewHTTPError(http.StatusRequestHeaderFieldsTooLarge)   // HTTP 431 Request Header Fields Too Large
	ErrUnavailableForLegalReasons    = NewHTTPError(http.StatusUnavailableForLegalReasons)    // HTTP 451 Unavailable For Legal Reasons
	ErrInternalServerError           = NewHTTPError(http.StatusInternalServerError)           // HTTP 500 Internal Server Error
	ErrNotImplemented                = NewHTTPError(http.StatusNotImplemented)                // HTTP 501 Not Implemented
	ErrBadGateway                    = NewHTTPError(http.StatusBadGateway)                    // HTTP 502 Bad Gateway
	ErrServiceUnavailable            = NewHTTPError(http.StatusServiceUnavailable)            // HTTP 503 Service Unavailable
	ErrGatewayTimeout                = NewHTTPError(http.StatusGatewayTimeout)                // HTTP 504 Gateway Timeout
	ErrHTTPVersionNotSupported       = NewHTTPError(http.StatusHTTPVersionNotSupported)       // HTTP 505 HTTP Version Not Supported
	ErrVariantAlsoNegotiates         = NewHTTPError(http.StatusVariantAlsoNegotiates)         // HTTP 506 Variant Also Negotiates
	ErrInsufficientStorage           = NewHTTPError(http.StatusInsufficientStorage)           // HTTP 507 Insufficient Storage
	ErrLoopDetected                  = NewHTTPError(http.StatusLoopDetected)                  // HTTP 508 Loop Detected
	ErrNotExtended                   = NewHTTPError(http.StatusNotExtended)                   // HTTP 510 Not Extended
	ErrNetworkAuthenticationRequired = NewHTTPError(http.StatusNetworkAuthenticationRequired) // HTTP 511 Network Authentication Required

	ErrValidatorNotRegistered = errors.New("validator not registered")
	ErrRendererNotRegistered  = errors.New("renderer not registered")
	ErrInvalidRedirectCode    = errors.New("invalid redirect status code")
	ErrCookieNotFound         = errors.New("cookie not found")
	ErrInvalidCertOrKeyType   = errors.New("invalid cert or key type, must be string or []byte")
	ErrInvalidListenerNetwork = errors.New("invalid listener network")
)

// NotFoundHandler is the handler that router uses in case there was no matching route found. Returns an error that results
// HTTP 404 status code.
var NotFoundHandler = func(c Context) error {
	return ErrNotFound
}

// MethodNotAllowedHandler is the handler thar router uses in case there was no matching route found but there was
// another matching routes for that requested URL. Returns an error that results HTTP 405 Method Not Allowed status code.
var MethodNotAllowedHandler = func(c Context) error {
	// See RFC 7231 section 7.4.1: An origin server MUST generate an Allow field in a 405 (Method Not Allowed)
	// response and MAY do so in any other response. For disabled resources an empty Allow header may be returned
	routerAllowMethods, ok := c.Get(ContextKeyHeaderAllow).(string)
	if ok && routerAllowMethods != "" {
		c.Response().Header().Set(HeaderAllow, routerAllowMethods)
	}
	return ErrMethodNotAllowed
}

// New creates an instance of Echo.
func New() (e *Echo) {
	e = &Echo{
		filesystem: createFilesystem(),
		Server:     new(http.Server),
		TLSServer:  new(http.Server),
		AutoTLSManager: autocert.Manager{
			Prompt: autocert.AcceptTOS,
		},
		//Logger:          log.New("echo"),
		//colorer:         color.New(),
		maxParam:        new(int),
		ListenerNetwork: "tcp",
	}
	e.Server.Handler = e
	e.TLSServer.Handler = e
	e.HTTPErrorHandler = e.DefaultHTTPErrorHandler
	e.Binder = &DefaultBinder{}
	e.JSONSerializer = &DefaultJSONSerializer{}
	//e.Logger.SetLevel(log.ERROR)
	//e.StdLogger = stdLog.New(e.Logger.Output(), e.Logger.Prefix()+": ", 0)
	e.SetLogger(new(SlogLogger))
	e.pool.New = func() interface{} {
		return e.NewContext(nil, nil)
	}
	e.router = NewRouter(e)
	e.routers = map[string]*Router{}
	return
}

// NewContext returns a Context instance.
func (e *Echo) NewContext(r *http.Request, w http.ResponseWriter) Context {
	return &context{
		request:  r,
		response: NewResponse(w, e),
		store:    make(Map),
		echo:     e,
		pvalues:  make([]string, *e.maxParam),
		handler:  NotFoundHandler,
	}
}

// Router returns the default router.
func (e *Echo) Router() *Router {
	return e.router
}

// Routers returns the map of host => router.
func (e *Echo) Routers() map[string]*Router {
	return e.routers
}

// DefaultHTTPErrorHandler is the default HTTP error handler. It sends a JSON response
// with status code.
//
// NOTE: In case errors happens in middleware call-chain that is returning from handler (which did not return an error).
// When handler has already sent response (ala c.JSON()) and there is error in middleware that is returning from
// handler. Then the error that global error handler received will be ignored because we have already "committed" the
// response and status code header has been sent to the client.
func (e *Echo) DefaultHTTPErrorHandler(err error, c Context) {

	if c.Response().Committed {
		return
	}

	he, ok := err.(*HTTPError)
	if ok {
		if he.Internal != nil {
			if herr, ok := he.Internal.(*HTTPError); ok {
				he = herr
			}
		}
	} else {
		he = &HTTPError{
			Code:    http.StatusInternalServerError,
			Message: http.StatusText(http.StatusInternalServerError),
		}
	}

	// Issue #1426
	code := he.Code
	message := he.Message

	switch m := he.Message.(type) {
	case string:
		if e.Debug {
			message = Map{"message": m, "error": err.Error()}
		} else {
			message = Map{"message": m}
		}
	case json.Marshaler:
		// do nothing - this type knows how to format itself to JSON
	case error:
		message = Map{"message": m.Error()}
	}

	// Send response
	if c.Request().Method == http.MethodHead { // Issue #608
		err = c.NoContent(he.Code)
	} else {
		err = c.JSON(code, message)
	}
	if err != nil {
		//e.Logger.Error(err)
		e.Logger.Error(fmt.Sprintf("%v", err))
	}
}

// Pre adds middleware to the chain which is run before router.
func (e *Echo) Pre(middleware ...MiddlewareFunc) {
	e.premiddleware = append(e.premiddleware, middleware...)
}

// Use adds middleware to the chain which is run after router.
func (e *Echo) Use(middleware ...MiddlewareFunc) {
	e.middleware = append(e.middleware, middleware...)
}

// CONNECT registers a new CONNECT route for a path with matching handler in the
// router with optional route-level middleware.
func (e *Echo) CONNECT(path string, h HandlerFunc, m ...MiddlewareFunc) *Route {
	return e.Add(http.MethodConnect, path, h, m...)
}

// DELETE registers a new DELETE route for a path with matching handler in the router
// with optional route-level middleware.
func (e *Echo) DELETE(path string, h HandlerFunc, m ...MiddlewareFunc) *Route {
	return e.Add(http.MethodDelete, path, h, m...)
}

// GET registers a new GET route for a path with matching handler in the router
// with optional route-level middleware.
func (e *Echo) GET(path string, h HandlerFunc, m ...MiddlewareFunc) *Route {
	return e.Add(http.MethodGet, path, h, m...)
}

// HEAD registers a new HEAD route for a path with matching handler in the
// router with optional route-level middleware.
func (e *Echo) HEAD(path string, h HandlerFunc, m ...MiddlewareFunc) *Route {
	return e.Add(http.MethodHead, path, h, m...)
}

// OPTIONS registers a new OPTIONS route for a path with matching handler in the
// router with optional route-level middleware.
func (e *Echo) OPTIONS(path string, h HandlerFunc, m ...MiddlewareFunc) *Route {
	return e.Add(http.MethodOptions, path, h, m...)
}

// PATCH registers a new PATCH route for a path with matching handler in the
// router with optional route-level middleware.
func (e *Echo) PATCH(path string, h HandlerFunc, m ...MiddlewareFunc) *Route {
	return e.Add(http.MethodPatch, path, h, m...)
}

// POST registers a new POST route for a path with matching handler in the
// router with optional route-level middleware.
func (e *Echo) POST(path string, h HandlerFunc, m ...MiddlewareFunc) *Route {
	return e.Add(http.MethodPost, path, h, m...)
}

// PUT registers a new PUT route for a path with matching handler in the
// router with optional route-level middleware.
func (e *Echo) PUT(path string, h HandlerFunc, m ...MiddlewareFunc) *Route {
	return e.Add(http.MethodPut, path, h, m...)
}

// TRACE registers a new TRACE route for a path with matching handler in the
// router with optional route-level middleware.
func (e *Echo) TRACE(path string, h HandlerFunc, m ...MiddlewareFunc) *Route {
	return e.Add(http.MethodTrace, path, h, m...)
}

// RouteNotFound registers a special-case route which is executed when no other route is found (i.e. HTTP 404 cases)
// for current request URL.
// Path supports static and named/any parameters just like other http method is defined. Generally path is ended with
// wildcard/match-any character (`/*`, `/download/*` etc).
//
// Example: `e.RouteNotFound("/*", func(c echo.Context) error { return c.NoContent(http.StatusNotFound) })`
func (e *Echo) RouteNotFound(path string, h HandlerFunc, m ...MiddlewareFunc) *Route {
	return e.Add(RouteNotFound, path, h, m...)
}

// Any registers a new route for all HTTP methods (supported by Echo) and path with matching handler
// in the router with optional route-level middleware.
//
// Note: this method only adds specific set of supported HTTP methods as handler and is not true
// "catch-any-arbitrary-method" way of matching requests.
func (e *Echo) Any(path string, handler HandlerFunc, middleware ...MiddlewareFunc) []*Route {
	routes := make([]*Route, len(methods))
	for i, m := range methods {
		routes[i] = e.Add(m, path, handler, middleware...)
	}
	return routes
}

// Match registers a new route for multiple HTTP methods and path with matching
// handler in the router with optional route-level middleware.
func (e *Echo) Match(methods []string, path string, handler HandlerFunc, middleware ...MiddlewareFunc) []*Route {
	routes := make([]*Route, len(methods))
	for i, m := range methods {
		routes[i] = e.Add(m, path, handler, middleware...)
	}
	return routes
}

func (common) file(path, file string, get func(string, HandlerFunc, ...MiddlewareFunc) *Route,
	m ...MiddlewareFunc) *Route {
	return get(path, func(c Context) error {
		return c.File(file)
	}, m...)
}

// File registers a new route with path to serve a static file with optional route-level middleware.
func (e *Echo) File(path, file string, m ...MiddlewareFunc) *Route {
	return e.file(path, file, e.GET, m...)
}

func (e *Echo) add(host, method, path string, handler HandlerFunc, middlewares ...MiddlewareFunc) *Route {
	router := e.findRouter(host)
	//FIXME: when handler+middleware are both nil ... make it behave like handler removal
	name := handlerName(handler)
	route := router.add(method, path, name, func(c Context) error {
		h := applyMiddleware(handler, middlewares...)
		return h(c)
	})

	if e.OnAddRouteHandler != nil {
		e.OnAddRouteHandler(host, *route, handler, middlewares)
	}

	return route
}

// Add registers a new route for an HTTP method and path with matching handler
// in the router with optional route-level middleware.
func (e *Echo) Add(method, path string, handler HandlerFunc, middleware ...MiddlewareFunc) *Route {
	return e.add("", method, path, handler, middleware...)
}

// Host creates a new router group for the provided host and optional host-level middleware.
func (e *Echo) Host(name string, m ...MiddlewareFunc) (g *Group) {
	e.routers[name] = NewRouter(e)
	g = &Group{host: name, echo: e}
	g.Use(m...)
	return
}

// Group creates a new router group with prefix and optional group-level middleware.
func (e *Echo) Group(prefix string, m ...MiddlewareFunc) (g *Group) {
	g = &Group{prefix: prefix, echo: e}
	g.Use(m...)
	return
}

// URI generates an URI from handler.
func (e *Echo) URI(handler HandlerFunc, params ...interface{}) string {
	name := handlerName(handler)
	return e.Reverse(name, params...)
}

// URL is an alias for `URI` function.
func (e *Echo) URL(h HandlerFunc, params ...interface{}) string {
	return e.URI(h, params...)
}

// Reverse generates a URL from route name and provided parameters.
func (e *Echo) Reverse(name string, params ...interface{}) string {
	return e.router.Reverse(name, params...)
}

// Routes returns the registered routes for default router.
// In case when Echo serves multiple hosts/domains use `e.Routers()["domain2.site"].Routes()` to get specific host routes.
func (e *Echo) Routes() []*Route {
	return e.router.Routes()
}

// AcquireContext returns an empty `Context` instance from the pool.
// You must return the context by calling `ReleaseContext()`.
func (e *Echo) AcquireContext() Context {
	return e.pool.Get().(Context)
}

// ReleaseContext returns the `Context` instance back to the pool.
// You must call it after `AcquireContext()`.
func (e *Echo) ReleaseContext(c Context) {
	e.pool.Put(c)
}

// ServeHTTP implements `http.Handler` interface, which serves HTTP requests.
func (e *Echo) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// Acquire context
	c := e.pool.Get().(*context)
	c.Reset(r, w)
	var h HandlerFunc

	if e.premiddleware == nil {
		e.findRouter(r.Host).Find(r.Method, GetPath(r), c)
		h = c.Handler()
		h = applyMiddleware(h, e.middleware...)
	} else {
		h = func(c Context) error {
			e.findRouter(r.Host).Find(r.Method, GetPath(r), c)
			h := c.Handler()
			h = applyMiddleware(h, e.middleware...)
			return h(c)
		}
		h = applyMiddleware(h, e.premiddleware...)
	}

	// Execute chain
	if err := h(c); err != nil {
		e.HTTPErrorHandler(err, c)
	}

	// Release context
	e.pool.Put(c)
}

// Start starts an HTTP server.
func (e *Echo) Start(address string) error {
	e.startupMutex.Lock()
	e.Server.Addr = address
	if err := e.configureServer(e.Server); err != nil {
		e.startupMutex.Unlock()
		return err
	}
	e.startupMutex.Unlock()
	return e.Server.Serve(e.Listener)
}

// StartTLS starts an HTTPS server.
// If `certFile` or `keyFile` is `string` the values are treated as file paths.
// If `certFile` or `keyFile` is `[]byte` the values are treated as the certificate or key as-is.
func (e *Echo) StartTLS(address string, certFile, keyFile interface{}) (err error) {
	e.startupMutex.Lock()
	var cert []byte
	if cert, err = filepathOrContent(certFile); err != nil {
		e.startupMutex.Unlock()
		return
	}

	var key []byte
	if key, err = filepathOrContent(keyFile); err != nil {
		e.startupMutex.Unlock()
		return
	}

	s := e.TLSServer
	s.TLSConfig = new(tls.Config)
	s.TLSConfig.Certificates = make([]tls.Certificate, 1)
	if s.TLSConfig.Certificates[0], err = tls.X509KeyPair(cert, key); err != nil {
		e.startupMutex.Unlock()
		return
	}

	e.configureTLS(address)
	if err := e.configureServer(s); err != nil {
		e.startupMutex.Unlock()
		return err
	}
	e.startupMutex.Unlock()
	return s.Serve(e.TLSListener)
}

func filepathOrContent(fileOrContent interface{}) (content []byte, err error) {
	switch v := fileOrContent.(type) {
	case string:
		return os.ReadFile(v)
	case []byte:
		return v, nil
	default:
		return nil, ErrInvalidCertOrKeyType
	}
}

// StartAutoTLS starts an HTTPS server using certificates automatically installed from https://letsencrypt.org.
func (e *Echo) StartAutoTLS(address string) error {
	e.startupMutex.Lock()
	s := e.TLSServer
	s.TLSConfig = new(tls.Config)
	s.TLSConfig.GetCertificate = e.AutoTLSManager.GetCertificate
	s.TLSConfig.NextProtos = append(s.TLSConfig.NextProtos, acme.ALPNProto)

	e.configureTLS(address)
	if err := e.configureServer(s); err != nil {
		e.startupMutex.Unlock()
		return err
	}
	e.startupMutex.Unlock()
	return s.Serve(e.TLSListener)
}

func (e *Echo) configureTLS(address string) {
	s := e.TLSServer
	s.Addr = address
	if !e.DisableHTTP2 {
		s.TLSConfig.NextProtos = append(s.TLSConfig.NextProtos, "h2")
	}
}

// StartServer starts a custom http server.
func (e *Echo) StartServer(s *http.Server) (err error) {
	e.startupMutex.Lock()
	if err := e.configureServer(s); err != nil {
		e.startupMutex.Unlock()
		return err
	}
	if s.TLSConfig != nil {
		e.startupMutex.Unlock()
		return s.Serve(e.TLSListener)
	}
	e.startupMutex.Unlock()
	return s.Serve(e.Listener)
}

func (e *Echo) configureServer(s *http.Server) error {
	// Setup
	//e.colorer.SetOutput(e.Logger.Output())
	s.ErrorLog = e.StdLogger
	s.Handler = e
	//if e.Debug {
	//	e.Logger.SetLevel(log.DEBUG)
	//}

	if !e.HideBanner {
		//e.colorer.Printf(banner, e.colorer.Red("v"+Version), e.colorer.Blue(website))
	}

	if s.TLSConfig == nil {
		if e.Listener == nil {
			l, err := newListener(s.Addr, e.ListenerNetwork)
			if err != nil {
				return err
			}
			e.Listener = l
		}
		if !e.HidePort {
			//e.colorer.Printf("⇨ http server started on %s\n", e.colorer.Green(e.Listener.Addr()))
			e.Logger.Info(fmt.Sprintf("⇨ http server started on %s", e.Listener.Addr()))
		}
		return nil
	}
	if e.TLSListener == nil {
		l, err := newListener(s.Addr, e.ListenerNetwork)
		if err != nil {
			return err
		}
		e.TLSListener = tls.NewListener(l, s.TLSConfig)
	}
	if !e.HidePort {
		//e.colorer.Printf("⇨ https server started on %s\n", e.colorer.Green(e.TLSListener.Addr()))
		e.Logger.Info(fmt.Sprintf("⇨ https server started on %s", e.TLSListener.Addr()))
	}
	return nil
}

// ListenerAddr returns net.Addr for Listener
func (e *Echo) ListenerAddr() net.Addr {
	e.startupMutex.RLock()
	defer e.startupMutex.RUnlock()
	if e.Listener == nil {
		return nil
	}
	return e.Listener.Addr()
}

// TLSListenerAddr returns net.Addr for TLSListener
func (e *Echo) TLSListenerAddr() net.Addr {
	e.startupMutex.RLock()
	defer e.startupMutex.RUnlock()
	if e.TLSListener == nil {
		return nil
	}
	return e.TLSListener.Addr()
}

// StartH2CServer starts a custom http/2 server with h2c (HTTP/2 Cleartext).
func (e *Echo) StartH2CServer(address string, h2s *http2.Server) error {
	e.startupMutex.Lock()
	// Setup
	s := e.Server
	s.Addr = address
	//e.colorer.SetOutput(e.Logger.Output())
	s.ErrorLog = e.StdLogger
	s.Handler = h2c.NewHandler(e, h2s)
	//if e.Debug {
	//	e.Logger.SetLevel(log.DEBUG)
	//}

	if !e.HideBanner {
		//e.colorer.Printf(banner, e.colorer.Red("v"+Version), e.colorer.Blue(website))
	}

	if e.Listener == nil {
		l, err := newListener(s.Addr, e.ListenerNetwork)
		if err != nil {
			e.startupMutex.Unlock()
			return err
		}
		e.Listener = l
	}
	if !e.HidePort {
		//e.colorer.Printf("⇨ http server started on %s\n", e.colorer.Green(e.Listener.Addr()))
		e.Logger.Info(fmt.Sprintf("⇨ http server started on %s", e.Listener.Addr()))
	}
	e.startupMutex.Unlock()
	return s.Serve(e.Listener)
}

// Close immediately stops the server.
// It internally calls `http.Server#Close()`.
func (e *Echo) Close() error {
	e.startupMutex.Lock()
	defer e.startupMutex.Unlock()
	if err := e.TLSServer.Close(); err != nil {
		return err
	}
	return e.Server.Close()
}

// Shutdown stops the server gracefully.
// It internally calls `http.Server#Shutdown()`.
func (e *Echo) Shutdown(ctx stdContext.Context) error {
	e.startupMutex.Lock()
	defer e.startupMutex.Unlock()
	if err := e.TLSServer.Shutdown(ctx); err != nil {
		return err
	}
	return e.Server.Shutdown(ctx)
}

// NewHTTPError creates a new HTTPError instance.
func NewHTTPError(code int, message ...interface{}) *HTTPError {
	he := &HTTPError{Code: code, Message: http.StatusText(code)}
	if len(message) > 0 {
		he.Message = message[0]
	}
	return he
}

// Error makes it compatible with `error` interface.
func (he *HTTPError) Error() string {
	if he.Internal == nil {
		return fmt.Sprintf("code=%d, message=%v", he.Code, he.Message)
	}
	return fmt.Sprintf("code=%d, message=%v, internal=%v", he.Code, he.Message, he.Internal)
}

// SetInternal sets error to HTTPError.Internal
func (he *HTTPError) SetInternal(err error) *HTTPError {
	he.Internal = err
	return he
}

// WithInternal returns clone of HTTPError with err set to HTTPError.Internal field
func (he *HTTPError) WithInternal(err error) *HTTPError {
	return &HTTPError{
		Code:     he.Code,
		Message:  he.Message,
		Internal: err,
	}
}

// Unwrap satisfies the Go 1.13 error wrapper interface.
func (he *HTTPError) Unwrap() error {
	return he.Internal
}

// WrapHandler wraps `http.Handler` into `echo.HandlerFunc`.
func WrapHandler(h http.Handler) HandlerFunc {
	return func(c Context) error {
		h.ServeHTTP(c.Response(), c.Request())
		return nil
	}
}

// WrapMiddleware wraps `func(http.Handler) http.Handler` into `echo.MiddlewareFunc`
func WrapMiddleware(m func(http.Handler) http.Handler) MiddlewareFunc {
	return func(next HandlerFunc) HandlerFunc {
		return func(c Context) (err error) {
			m(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				c.SetRequest(r)
				c.SetResponse(NewResponse(w, c.Echo()))
				err = next(c)
			})).ServeHTTP(c.Response(), c.Request())
			return
		}
	}
}

// GetPath returns RawPath, if it's empty returns Path from URL
// Difference between RawPath and Path is:
//   - Path is where request path is stored. Value is stored in decoded form: /%47%6f%2f becomes /Go/.
//   - RawPath is an optional field which only gets set if the default encoding is different from Path.
func GetPath(r *http.Request) string {
	path := r.URL.RawPath
	if path == "" {
		path = r.URL.Path
	}
	return path
}

func (e *Echo) findRouter(host string) *Router {
	if len(e.routers) > 0 {
		if r, ok := e.routers[host]; ok {
			return r
		}
	}
	return e.router
}

func handlerName(h HandlerFunc) string {
	t := reflect.ValueOf(h).Type()
	if t.Kind() == reflect.Func {
		return runtime.FuncForPC(reflect.ValueOf(h).Pointer()).Name()
	}
	return t.String()
}

// // PathUnescape is wraps `url.PathUnescape`
// func PathUnescape(s string) (string, error) {
// 	return url.PathUnescape(s)
// }

// tcpKeepAliveListener sets TCP keep-alive timeouts on accepted
// connections. It's used by ListenAndServe and ListenAndServeTLS so
// dead TCP connections (e.g. closing laptop mid-download) eventually
// go away.
type tcpKeepAliveListener struct {
	*net.TCPListener
}

func (ln tcpKeepAliveListener) Accept() (c net.Conn, err error) {
	if c, err = ln.AcceptTCP(); err != nil {
		return
	} else if err = c.(*net.TCPConn).SetKeepAlive(true); err != nil {
		return
	}
	// Ignore error from setting the KeepAlivePeriod as some systems, such as
	// OpenBSD, do not support setting TCP_USER_TIMEOUT on IPPROTO_TCP
	_ = c.(*net.TCPConn).SetKeepAlivePeriod(3 * time.Minute)
	return
}

func newListener(address, network string) (*tcpKeepAliveListener, error) {
	if network != "tcp" && network != "tcp4" && network != "tcp6" {
		return nil, ErrInvalidListenerNetwork
	}
	l, err := net.Listen(network, address)
	if err != nil {
		return nil, err
	}
	return &tcpKeepAliveListener{l.(*net.TCPListener)}, nil
}

func applyMiddleware(h HandlerFunc, middleware ...MiddlewareFunc) HandlerFunc {
	for i := len(middleware) - 1; i >= 0; i-- {
		h = middleware[i](h)
	}
	return h
}
