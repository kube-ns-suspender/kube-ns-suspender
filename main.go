package main

import (
	"context"
	"fmt"
	"log"
	"time"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	//
	// Uncomment to load all auth plugins
	// _ "k8s.io/client-go/plugin/pkg/client/auth"
	//
	// Or uncomment to load specific auth plugins
	// _ "k8s.io/client-go/plugin/pkg/client/auth/azure"
	// _ "k8s.io/client-go/plugin/pkg/client/auth/gcp"
	// _ "k8s.io/client-go/plugin/pkg/client/auth/oidc"
	// _ "k8s.io/client-go/plugin/pkg/client/auth/openstack"
)

func main() {
	// creates the in-cluster config
	config, err := rest.InClusterConfig()
	if err != nil {
		panic(err.Error())
	}
	// creates the clientset
	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		panic(err.Error())
	}
	for {
		fmt.Println("=-= NEW SCAN =-=")
		// get all namespaces
		ns, err := clientset.CoreV1().Namespaces().List(context.TODO(), metav1.ListOptions{})
		if err != nil {
			panic(err.Error())
		}

		// display all namespaces
		fmt.Println("Namespaces:")
		for _, n := range ns.Items {
			fmt.Printf("  * %s\n", n.Name)
		}
		fmt.Println()

		// display all namespaces with annotation kube-ns-suspender: "true"
		var feNamespaces []v1.Namespace
		fmt.Println("Namespaces using FEscaler:")
		for _, n := range ns.Items {
			if v, ok := n.Annotations["kube-ns-suspender"]; ok {
				if v == "true" {
					fmt.Printf("  * %s\n", n.Name)
					feNamespaces = append(feNamespaces, n)
				}
			}
		}
		fmt.Println()

		// display all deployments in namespaces that have kube-ns-suspender
		for _, ns := range feNamespaces {
			fmt.Printf("Deployments in %s:\n", ns.Name)
			depl, err := clientset.AppsV1().Deployments(ns.Name).List(context.TODO(), metav1.ListOptions{})
			if err != nil {
				panic(err.Error())
			}
			for _, d := range depl.Items {
				if d.Name != "kube-ns-suspender-depl" { // debug purposes
					numReplicas := int(*d.Spec.Replicas)
					fmt.Printf("  * %s (repl: %d)\n", d.Name, numReplicas)
					s, err := clientset.AppsV1().Deployments(ns.Name).GetScale(context.TODO(), d.Name, metav1.GetOptions{})
					if err != nil {
						panic(err.Error())
					}
					sc := *s
					sc.Spec.Replicas = int32(numReplicas) + 1
					_, err = clientset.AppsV1().Deployments(ns.Name).UpdateScale(context.TODO(), d.Name, &sc, metav1.UpdateOptions{})
					if err != nil {
						log.Fatal(err)
					}
				}
			}
		}
		fmt.Println()
		time.Sleep(1 * time.Minute)
	}
}
