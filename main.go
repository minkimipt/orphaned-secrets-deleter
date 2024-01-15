package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/util/homedir"
)

func main() {
	var allNamespaces, dryRun bool
	var namespace string

	flag.BoolVar(&allNamespaces, "all", false, "Delete secrets in all namespaces with the label cloud.timescale.com/is-customer-resource=\"true\"")
	flag.BoolVar(&dryRun, "dry-run", false, "Print messages without deleting secrets")
	flag.StringVar(&namespace, "namespace", "", "namespace to clean up secrets")

	flag.Parse()
	kubeconfig := filepath.Join(
		os.Getenv("HOME"), ".kube", "config",
	)
	if namespace == "" && !allNamespaces {
		fmt.Println("Please specify the namespace using the -namespace flag.")
		os.Exit(1)
	}

	config, err := clientcmd.BuildConfigFromFlags("", kubeconfig)
	if err != nil {
		fmt.Printf("Error building kubeconfig: %v\n", err)
		os.Exit(1)
	}

	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		fmt.Printf("Error creating kubernetes client: %v\n", err)
		os.Exit(1)
	}

	if allNamespaces {
		err := cleanupAllNamespaces(clientset, dryRun)
		if err != nil {
			fmt.Printf("Error cleaning up all namespaces: %v\n", err)
			os.Exit(1)
		}
	} else {
		err := cleanupSecrets(clientset, namespace, dryRun)
		if err != nil {
			fmt.Printf("Error cleaning up namespace %s: %v\n", namespace, err)
			os.Exit(1)
		}
	}
}

func cleanupAllNamespaces(clientset *kubernetes.Clientset, dryRun bool) error {
	namespaces, err := clientset.CoreV1().Namespaces().List(context.TODO(), metav1.ListOptions{
		LabelSelector: "cloud.timescale.com/is-customer-resource=true",
	})
	if err != nil {
		return fmt.Errorf("error listing namespaces: %v", err)
	}

	for _, namespace := range namespaces.Items {
		err := cleanupSecrets(clientset, namespace.Name, dryRun)
		if err != nil {
			fmt.Printf("Error cleaning up namespace %s: %v\n", namespace.Name, err)
		}
	}

	return nil
}

func cleanupSecrets(clientset *kubernetes.Clientset, namespace string, dryRun bool) error {
	secrets, err := clientset.CoreV1().Secrets(namespace).List(context.TODO(), metav1.ListOptions{})
	if err != nil {
		return fmt.Errorf("error listing secrets: %v", err)
	}

	for _, secret := range secrets.Items {
		if strings.HasSuffix(secret.Name, "-certificate") && isEmptyOwnerReference(secret) && !strings.Contains(secret.Name, "root") {
			fmt.Printf("Deleting secret %s as it is not used by any pods\n", secret.Name)
			if !dryRun {
				if err := clientset.CoreV1().Secrets(namespace).Delete(context.TODO(), secret.Name, metav1.DeleteOptions{}); err != nil {
					fmt.Printf("Error deleting secret %s: %v\n", secret.Name, err)
				}
			}
		}
	}

	return nil
}

func isEmptyOwnerReference(secret v1.Secret) bool {
	return len(secret.OwnerReferences) == 0
}

func getDefaultKubeconfigPath() string {
	home := homedir.HomeDir()
	return filepath.Join(home, ".kube", "config")
}
