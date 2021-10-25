package webui

import (
	"context"
	"html/template"
	"log"
	"net/http"

	"github.com/gorilla/mux"
	"github.com/rs/zerolog"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/util/retry"
)

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

// ugly
var cs *kubernetes.Clientset

func Start(l zerolog.Logger, port string) error {
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
		Handler: createRouter(l),
	}
	if err := srv.ListenAndServe(); err != nil {
		return err
	}
	return nil
}

func createRouter(l zerolog.Logger) *mux.Router {
	r := mux.NewRouter()
	withLogger := loggingHandlerFactory(l)
	r.Handle("/", withLogger(homePage)).Methods(http.MethodGet)
	r.Handle("/unsuspend", withLogger(unsuspendHandler)).Methods(http.MethodPost)
	r.Handle("/bug", withLogger(bugHandler)).Methods(http.MethodGet)
	r.Handle("/list", withLogger(listHandler)).Methods(http.MethodGet)
	r.NotFoundHandler = http.HandlerFunc(errorPage)

	return r
}

func homePage(w http.ResponseWriter, r *http.Request, l zerolog.Logger) {
	tmpl, err := template.ParseFiles("webui/assets/home.html", "webui/assets/templates/head.html",
		"webui/assets/templates/style.html", "webui/assets/templates/footer.html")
	if err != nil {
		l.Error().Err(err).Str("page", "/").Msg("cannot parse files")
	}

	namespaces, err := cs.CoreV1().Namespaces().List(context.TODO(), v1.ListOptions{})
	if err != nil {
		l.Error().Err(err).Str("page", "/").Msg("cannot list namespaces")
	}

	var nsList NamespacesList
	for _, n := range namespaces.Items {
		if n.Annotations["kube-ns-suspender/desiredState"] == "Suspended" {
			nsList.Names = append(nsList.Names, n.Name)
		}
	}

	if len(nsList.Names) == 0 {
		nsList.IsEmpty = true
	} else {
		nsList.IsEmpty = false
	}
	err = tmpl.Execute(w, nsList)
	if err != nil {
		l.Error().Err(err).Str("page", "/").Msg("cannot execute template")
	}
}

func unsuspendHandler(w http.ResponseWriter, r *http.Request, l zerolog.Logger) {
	tmpl, err := template.ParseFiles("webui/assets/unsuspend.html", "webui/assets/templates/head.html",
		"webui/assets/templates/style.html", "webui/assets/templates/footer.html")
	if err != nil {
		l.Error().Err(err).Str("page", "/unsuspend").Msg("cannot parse files")
	}

	uns := UnsuspendedNamespace{
		Name: r.FormValue("namespaces"),
	}
	if uns.Name == "ignore" {
		uns.Success = false
		uns.ErrorMsg = "you must select a namespace"
	} else {
		uns.Success, uns.Error = patchNamespace(uns.Name)
		if err != nil {
			uns.ErrorMsg = uns.Error.Error()
		}
	}

	if uns.Success {
		l.Info().Str("page", "/unsuspend").Msgf("unsuspended namespace %s using web ui", uns.Name)
	} else {
		l.Error().Err(uns.Error).Str("page", "/unsuspend").Msgf("error trying to unsuspend namespace %s from web ui", uns.Name)
	}
	err = tmpl.Execute(w, uns)
	if err != nil {
		l.Error().Err(err).Str("page", "/unsuspend").Msg("cannot execute template")
	}
}

func bugHandler(w http.ResponseWriter, r *http.Request, l zerolog.Logger) {
	tmpl, err := template.ParseFiles("webui/assets/bug.html", "webui/assets/templates/head.html",
		"webui/assets/templates/style.html", "webui/assets/templates/footer.html")
	if err != nil {
		l.Error().Err(err).Str("page", "/bug").Msg("cannot parse files")
	}

	err = tmpl.Execute(w, nil)
	if err != nil {
		l.Error().Err(err).Str("page", "/bug").Msg("cannot execute template")
	}
}

func errorPage(w http.ResponseWriter, r *http.Request) {
	tmpl, err := template.ParseFiles("webui/assets/404.html", "webui/assets/templates/head.html",
		"webui/assets/templates/style.html", "webui/assets/templates/footer.html")
	if err != nil {
		log.Fatalf("Can not parse home page : %v", err)
	}

	w.WriteHeader(http.StatusNotFound)
	err = tmpl.Execute(w, nil)
	if err != nil {
		log.Fatalf("Can not execute templates for home page : %v", err)
	}
}

func listHandler(w http.ResponseWriter, r *http.Request, l zerolog.Logger) {
	tmpl, err := template.ParseFiles("webui/assets/list.html", "webui/assets/templates/head.html",
		"webui/assets/templates/style.html", "webui/assets/templates/footer.html")
	if err != nil {
		l.Error().Err(err).Str("page", "/list").Msg("cannot parse files")
	}

	namespaces, err := cs.CoreV1().Namespaces().List(context.TODO(), v1.ListOptions{})
	if err != nil {
		l.Error().Err(err).Str("page", "/").Msg("cannot list namespaces")
	}

	var nsList ListNamespacesAndStates
	for _, n := range namespaces.Items {
		if val, ok := n.Annotations["kube-ns-suspender/desiredState"]; ok {
			ns := Namespace{Name: n.Name}
			switch val {
			case "Suspended":
				ns.State = "❌"
			case "Running", "RunningForced":
				ns.State = "✅"
			default:
				ns.State = "❔"
			}
			nsList.Namespaces = append(nsList.Namespaces, ns)
		}
	}
	err = tmpl.Execute(w, nsList)
	if err != nil {
		l.Error().Err(err).Str("page", "/list").Msg("cannot execute template")
	}
}

func patchNamespace(name string) (bool, error) {
	err := retry.RetryOnConflict(retry.DefaultRetry, func() error {
		result, err := cs.CoreV1().Namespaces().Get(context.TODO(), name, metav1.GetOptions{})
		if err != nil {
			return err
		}
		result.Annotations["kube-ns-suspender/desiredState"] = "RunningForced" // TODO: find a way to use the constants from engine package?
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
