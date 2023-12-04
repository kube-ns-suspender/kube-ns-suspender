package webui

import (
	"context"
	"embed"
	"fmt"
	"html/template"
	"net/http"
	"time"

	"github.com/gorilla/mux"
	"github.com/govirtuo/kube-ns-suspender/engine"
	"github.com/rs/zerolog"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/util/retry"
)

// assets holds our static web server assets.
//go:embed assets/*
var assets embed.FS

type Page struct {
	Error, HasMessage bool
	ErrMsg, Message   string
	NamespacesList    NamespacesList
	CurrentNamespace  Namespace
	Version           string
	BuildDate         string
	SlackChannelName  string
	SlackChannelLink  string
}

type NamespacesList struct {
	IsEmpty    bool
	Namespaces []Namespace
}

type UnsuspendedNamespace struct {
	Name string
}

type Namespace struct {
	Name             string
	State            string
	DailySuspendTime string
	NextSuspendTime  string
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
	slackChannelName   string
	slackChannelLink   string
}

var cs *kubernetes.Clientset

// Start starts the webui HTTP server
func Start(l zerolog.Logger, port, prefix, cn, v, bd, slackname, slacklink string) error {
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
		Handler: createRouter(l, prefix, cn, v, bd, slackname, slacklink),
	}
	if err := srv.ListenAndServe(); err != nil {
		return err
	}
	return nil
}

// createRouter creates the router with all the HTTP routes.
// It also passes different common values to the handlers
func createRouter(l zerolog.Logger, prefix, cn, v, bd, slackname, slacklink string) *mux.Router {
	r := mux.NewRouter()

	if v == "" {
		v = "n/a"
	}

	// add a # in front of the slack channel if not present. This is purely
	// for esthetics
	if slackname != "" && slackname[0] != '#' {
		slackname = "#" + slackname
	}

	h := handler{
		prefix:           prefix,
		controllerName:   cn,
		version:          v,
		builddate:        bd,
		slackChannelName: slackname,
		slackChannelLink: slacklink,
	}

	withLogger := loggingHandlerFactory(l)
	r.Handle("/", withLogger(h.homePage)).Methods(http.MethodGet)
	r.Handle("/suspend", withLogger(h.suspendPage)).Methods(http.MethodGet)
	r.Handle("/unsuspend", withLogger(h.unsuspendPage)).Methods(http.MethodGet)
	r.Handle("/bug", withLogger(h.bugPage)).Methods(http.MethodGet)
	r.NotFoundHandler = withLogger(h.errorPage)

	return r
}

// unsuspendPage handlers the POST requests done by users to unsuspend a given
// namespace selected on the home page
func (h handler) unsuspendPage(w http.ResponseWriter, r *http.Request, l zerolog.Logger) {
	tmpl, err := template.ParseFS(assets, "assets/action.html", "assets/_head.html",
		"assets/_style.html", "assets/_navbar.html")
	if err != nil {
		l.Error().Err(err).Str("page", "/unsuspend").Msg("cannot parse files")
	}

	p := Page{
		Version:   h.version,
		BuildDate: h.builddate,
	}

	vals, ok := r.URL.Query()["name"]
	if !ok || len(vals[0]) < 1 {
		p.Error = true
		p.ErrMsg = "One 'name' parameter is accepted."
		if err := tmpl.Execute(w, p); err != nil {
			l.Error().Err(err).Str("page", "/unsuspend").Msg("cannot execute template")
		}
		return
	}

	p.CurrentNamespace = Namespace{
		Name: vals[0],
	}
	if p.CurrentNamespace.Name == "ignore" {
		p.Error = false
		p.ErrMsg = "you must select a namespace"
	} else {
		err := patchNamespace(p.CurrentNamespace.Name, h.prefix, engine.Running)
		if err != nil {
			p.Error = true
			p.ErrMsg = err.Error()
			err = tmpl.Execute(w, p)
			if err != nil {
				l.Error().Err(err).Str("page", "/unsuspend").Msg("cannot execute template")
			}
			return
		}
	}

	p.HasMessage = true
	p.Message = fmt.Sprintf("Namespace %s successfully unsuspended.", p.CurrentNamespace.Name)
	l.Info().Str("page", "/unsuspend").Msgf("unsuspended namespace %s using web ui", p.CurrentNamespace.Name)
	err = tmpl.Execute(w, p)
	if err != nil {
		l.Error().Err(err).Str("page", "/unsuspend").Msg("cannot execute template")
	}
}

func (h handler) suspendPage(w http.ResponseWriter, r *http.Request, l zerolog.Logger) {
	p := Page{
		Version:   h.version,
		BuildDate: h.builddate,
	}

	tmpl, err := template.ParseFS(assets, "assets/action.html", "assets/_head.html",
		"assets/_style.html", "assets/_navbar.html")
	if err != nil {
		l.Error().Err(err).Str("page", "/suspend").Msg("cannot parse files")
		p.Error = true
		p.ErrMsg = "Cannot parse template files: " + err.Error()
		if err := tmpl.Execute(w, p); err != nil {
			l.Error().Err(err).Str("page", "/suspend").Msg("cannot execute template")
		}
		return
	}

	vals, ok := r.URL.Query()["name"]
	if !ok || len(vals[0]) < 1 {
		p.Error = true
		p.ErrMsg = "One 'name' parameter is accepted."
		if err := tmpl.Execute(w, p); err != nil {
			l.Error().Err(err).Str("page", "/suspend").Msg("cannot execute template")
		}
		return
	}

	p.CurrentNamespace = Namespace{
		Name: vals[0],
	}
	if p.CurrentNamespace.Name == "ignore" {
		p.Error = false
		p.ErrMsg = "you must select a namespace"
	} else {
		err := patchNamespace(p.CurrentNamespace.Name, h.prefix, engine.Suspended)
		if err != nil {
			p.Error = true
			p.ErrMsg = err.Error()
			err = tmpl.Execute(w, p)
			if err != nil {
				l.Error().Err(err).Str("page", "/suspended").Msg("cannot execute template")
			}
			return
		}
	}

	p.HasMessage = true
	p.Message = fmt.Sprintf("Namespace %s successfully suspended.", p.CurrentNamespace.Name)
	l.Info().Str("page", "/suspended").Msgf("suspended namespace %s using web ui", p.CurrentNamespace.Name)
	err = tmpl.Execute(w, p)
	if err != nil {
		l.Error().Err(err).Str("page", "/suspended").Msg("cannot execute template")
	}
}

// bugPage handles the pages with contact informations in case of a bug
func (h handler) bugPage(w http.ResponseWriter, r *http.Request, l zerolog.Logger) {
	tmpl, err := template.ParseFS(assets, "assets/bug.html", "assets/_head.html",
		"assets/_style.html", "assets/_navbar.html")
	if err != nil {
		l.Error().Err(err).Str("page", "/bug").Msg("cannot parse files")
	}

	p := Page{
		Version:          h.version,
		BuildDate:        h.builddate,
		SlackChannelName: h.slackChannelName,
		SlackChannelLink: h.slackChannelLink,
	}
	err = tmpl.Execute(w, p)
	if err != nil {
		l.Error().Err(err).Str("page", "/bug").Msg("cannot execute template")
	}
}

// errorPage handles the various 404 errors that can occur
func (h handler) errorPage(w http.ResponseWriter, r *http.Request, l zerolog.Logger) {
	tmpl, err := template.ParseFS(assets, "assets/404.html", "assets/_head.html",
		"assets/_style.html", "assets/_navbar.html")
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

// homePage handles the / route that contains the list of namespaces, their
// state, a searchbar etc...
func (h handler) homePage(w http.ResponseWriter, r *http.Request, l zerolog.Logger) {
	tmpl, err := template.ParseFS(assets, "assets/home.html", "assets/_head.html",
		"assets/_style.html", "assets/_navbar.html")
	if err != nil {
		l.Error().Err(err).Str("page", "/").Msg("cannot parse files")
	}

	namespaces, err := cs.CoreV1().Namespaces().List(context.TODO(), metav1.ListOptions{})
	if err != nil {
		l.Error().Err(err).Str("page", "/").Msg("cannot list namespaces")
	}

	p := Page{
		Version:   h.version,
		BuildDate: h.builddate,
	}
	// var nsList ListNamespacesAndStates
	for _, n := range namespaces.Items {
		phase := fmt.Sprint(n.Status.Phase)
		if phase == "Terminating" {
			continue
		}
		if a, ok := n.Annotations[h.prefix+engine.ControllerName]; ok && a == h.controllerName {
			val := n.Annotations[h.prefix+engine.DesiredState]
			ns := Namespace{
				Name:             n.Name,
				DailySuspendTime: "n/a",
				NextSuspendTime:  "n/a",
				State:            val,
			}

			// add dailySuspendTime if it exists
			if dst, ok := n.Annotations[h.prefix+engine.DailySuspendTime]; ok {
				dstTime, err := time.Parse(time.Kitchen, dst)
				if err != nil {
					l.Error().Err(err).Str("page", "/").Str("namespace", n.Name).Msgf("cannot parse %s", engine.DailySuspendTime)
				} else {
					ns.DailySuspendTime = dstTime.Format(time.Kitchen)
				}
			}

			// add nextSuspendTime if it exists
			if nst, ok := n.Annotations[h.prefix+engine.NextSuspendTime]; ok {
				nstTime, err := time.Parse(time.RFC822Z, nst)
				if err != nil {
					l.Error().Err(err).Str("page", "/").Str("namespace", n.Name).Msgf("cannot parse %s", engine.NextSuspendTime)
				} else {
					ns.NextSuspendTime = nstTime.Format(time.RFC822)
				}
			}

			p.NamespacesList.Namespaces = append(p.NamespacesList.Namespaces, ns)
		}
	}
	err = tmpl.Execute(w, p)
	if err != nil {
		l.Error().Err(err).Str("page", "/").Msg("cannot execute template")
	}
}

func patchNamespace(name, prefix, state string) error {
	err := retry.RetryOnConflict(retry.DefaultRetry, func() error {
		result, err := cs.CoreV1().Namespaces().Get(context.TODO(), name, metav1.GetOptions{})
		if err != nil {
			return err
		}
		result.Annotations[prefix+engine.DesiredState] = state
		var updateOpts metav1.UpdateOptions
		_, err = cs.CoreV1().Namespaces().Update(context.TODO(), result, updateOpts)
		return err
	})

	if err != nil {
		return err
	}
	return nil
}

func (lh *loggingHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	lh.handlerFunc(w, r, lh.logger)
}

func loggingHandlerFactory(l zerolog.Logger) func(loggingHandlerFunc) *loggingHandler {
	return func(hf loggingHandlerFunc) *loggingHandler {
		return &loggingHandler{l, hf}
	}
}
