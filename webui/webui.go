package webui

import (
	"context"
	"embed"
	"html/template"
	"net/http"

	"github.com/gorilla/mux"
	"github.com/govirtuo/kube-ns-suspender/engine"
	"github.com/rs/zerolog"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/util/retry"
)

// assets holds our static web server assets.
//go:embed assets/*
var assets embed.FS

type Page struct {
	NL        NamespacesList
	UN        UnsuspendedNamespace
	LNS       ListNamespacesAndStates
	Version   string
	BuildDate string
}

type NamespacesList struct {
	IsEmpty bool
	Names   []string
}

type UnsuspendedNamespace struct {
	Success  bool
	Name     string
	Error    error
	ErrorMsg string
}

type ListNamespacesAndStates struct {
	Namespaces []Namespace
}

type Namespace struct {
	Name  string
	State string
}

type loggingHandlerFunc = func(w http.ResponseWriter, r *http.Request, l zerolog.Logger)

type loggingHandler struct {
	logger      zerolog.Logger
	handlerFunc loggingHandlerFunc
}

// this struct allows us to propagate the prefix variable
// into the HTTP handlers
type handler struct {
	prefix             string
	controllerName     string
	version, builddate string
}

var cs *kubernetes.Clientset

// Start starts the webui HTTP server
func Start(l zerolog.Logger, port, prefix, cn, v, bd string) error {
	// create the in-cluster config
	config, err := rest.InClusterConfig()
	if err != nil {
		return err
	}

	// create the clientset
	cs, err = kubernetes.NewForConfig(config)
	if err != nil {
		return err
	}

	srv := http.Server{
		Addr:    ":" + port,
		Handler: createRouter(l, prefix, cn, v, bd),
	}
	if err := srv.ListenAndServe(); err != nil {
		return err
	}
	return nil
}

// createRouter creates the router with all the HTTP routes.
// It also passes different common values to the handlers
func createRouter(l zerolog.Logger, prefix, cn, v, bd string) *mux.Router {
	r := mux.NewRouter()

	if v == "" {
		v = "n/a"
	}
	h := handler{
		prefix:         prefix,
		controllerName: cn,
		version:        v,
		builddate:      bd,
	}

	withLogger := loggingHandlerFactory(l)
	r.Handle("/", withLogger(h.homePage)).Methods(http.MethodGet)
	r.Handle("/unsuspend", withLogger(h.unsuspendHandler)).Methods(http.MethodPost)
	r.Handle("/bug", withLogger(h.bugPage)).Methods(http.MethodGet)
	r.Handle("/list", withLogger(h.listPage)).Methods(http.MethodGet)
	r.NotFoundHandler = withLogger(h.errorPage)

	return r
}

// homePage handles the home page, with the drop-down menu to unsuspend a namespace
func (h handler) homePage(w http.ResponseWriter, r *http.Request, l zerolog.Logger) {
	tmpl, err := template.ParseFS(assets, "assets/home.html", "assets/templates/head.html",
		"assets/templates/style.html", "assets/templates/footer.html")
	if err != nil {
		l.Error().Err(err).Str("page", "/").Msg("cannot parse files")
	}

	namespaces, err := cs.CoreV1().Namespaces().List(context.TODO(), v1.ListOptions{})
	if err != nil {
		l.Error().Err(err).Str("page", "/").Msg("cannot list namespaces")
	}

	p := Page{
		Version:   h.version,
		BuildDate: h.builddate,
	}
	// var nsList NamespacesList
	for _, n := range namespaces.Items {
		if n.Annotations[h.prefix+engine.DesiredState] == engine.Suspended {
			p.NL.Names = append(p.NL.Names, n.Name)
		}
	}

	if len(p.NL.Names) == 0 {
		p.NL.IsEmpty = true
	} else {
		p.NL.IsEmpty = false
	}
	err = tmpl.Execute(w, p)
	if err != nil {
		l.Error().Err(err).Str("page", "/").Msg("cannot execute template")
	}
}

// unsuspendHandler handlers the POST requests done by users to unsuspend a given
// namespace selected on the home page
func (h handler) unsuspendHandler(w http.ResponseWriter, r *http.Request, l zerolog.Logger) {
	tmpl, err := template.ParseFS(assets, "assets/unsuspend.html", "assets/templates/head.html",
		"assets/templates/style.html", "assets/templates/footer.html")
	if err != nil {
		l.Error().Err(err).Str("page", "/unsuspend").Msg("cannot parse files")
	}

	p := Page{
		Version:   h.version,
		BuildDate: h.builddate,
	}
	p.UN = UnsuspendedNamespace{
		Name: r.FormValue("namespaces"),
	}
	if p.UN.Name == "ignore" {
		p.UN.Success = false
		p.UN.ErrorMsg = "you must select a namespace"
	} else {
		p.UN.Success, p.UN.Error = patchNamespace(p.UN.Name, h.prefix)
		if err != nil {
			p.UN.ErrorMsg = p.UN.Error.Error()
		}
	}

	if p.UN.Success {
		l.Info().Str("page", "/unsuspend").Msgf("unsuspended namespace %s using web ui", p.UN.Name)
	} else {
		l.Error().Err(p.UN.Error).Str("page", "/unsuspend").Msgf("error trying to unsuspend namespace %s from web ui", p.UN.Name)
	}
	err = tmpl.Execute(w, p)
	if err != nil {
		l.Error().Err(err).Str("page", "/unsuspend").Msg("cannot execute template")
	}
}

// bugPage handles the pages with contact informations in case of a bug
func (h handler) bugPage(w http.ResponseWriter, r *http.Request, l zerolog.Logger) {
	tmpl, err := template.ParseFS(assets, "assets/bug.html", "assets/templates/head.html",
		"assets/templates/style.html", "assets/templates/footer.html")
	if err != nil {
		l.Error().Err(err).Str("page", "/bug").Msg("cannot parse files")
	}

	p := Page{
		Version:   h.version,
		BuildDate: h.builddate,
	}
	err = tmpl.Execute(w, p)
	if err != nil {
		l.Error().Err(err).Str("page", "/bug").Msg("cannot execute template")
	}
}

// errorPage handles the various 404 errors that can occur
func (h handler) errorPage(w http.ResponseWriter, r *http.Request, l zerolog.Logger) {
	tmpl, err := template.ParseFS(assets, "assets/404.html", "assets/templates/head.html",
		"assets/templates/style.html", "assets/templates/footer.html")
	if err != nil {
		l.Error().Err(err).Str("page", "/bug").Msg("cannot parse templates")
	}

	p := Page{
		Version:   h.version,
		BuildDate: h.builddate,
	}
	w.WriteHeader(http.StatusNotFound)
	err = tmpl.Execute(w, p)
	if err != nil {
		l.Error().Err(err).Str("page", "/bug").Msg("cannot execute templates")
	}
}

// listPage handles the /list route that contains the list of namespaces, their
// state, a searchbar etc...
func (h handler) listPage(w http.ResponseWriter, r *http.Request, l zerolog.Logger) {
	tmpl, err := template.ParseFS(assets, "assets/list.html", "assets/templates/head.html",
		"assets/templates/style.html", "assets/templates/footer.html")
	if err != nil {
		l.Error().Err(err).Str("page", "/list").Msg("cannot parse files")
	}

	namespaces, err := cs.CoreV1().Namespaces().List(context.TODO(), v1.ListOptions{})
	if err != nil {
		l.Error().Err(err).Str("page", "/list").Msg("cannot list namespaces")
	}

	p := Page{
		Version:   h.version,
		BuildDate: h.builddate,
	}
	// var nsList ListNamespacesAndStates
	for _, n := range namespaces.Items {
		if a, ok := n.Annotations[h.prefix+engine.ControllerName]; ok && a == h.controllerName {
			val := n.Annotations[h.prefix+engine.DesiredState]
			ns := Namespace{Name: n.Name}
			switch val {
			case engine.Suspended:
				ns.State = "🔴"
			case engine.Running, "":
				ns.State = "🟢"
			default:
				ns.State = "❔"
			}
			p.LNS.Namespaces = append(p.LNS.Namespaces, ns)
		}
	}
	err = tmpl.Execute(w, p)
	if err != nil {
		l.Error().Err(err).Str("page", "/list").Msg("cannot execute template")
	}
}

func patchNamespace(name, prefix string) (bool, error) {
	err := retry.RetryOnConflict(retry.DefaultRetry, func() error {
		result, err := cs.CoreV1().Namespaces().Get(context.TODO(), name, metav1.GetOptions{})
		if err != nil {
			return err
		}
		result.Annotations[prefix+engine.DesiredState] = engine.Running
		var updateOpts metav1.UpdateOptions
		_, err = cs.CoreV1().Namespaces().Update(context.TODO(), result, updateOpts)
		return err
	})

	if err != nil {
		return false, err
	}
	return true, nil
}

func (lh *loggingHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	lh.handlerFunc(w, r, lh.logger)
}

func loggingHandlerFactory(l zerolog.Logger) func(loggingHandlerFunc) *loggingHandler {
	return func(hf loggingHandlerFunc) *loggingHandler {
		return &loggingHandler{l, hf}
	}
}
