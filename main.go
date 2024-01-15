package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/util/homedir"
)

func main() {
	var kubeconfig string
	var namespace string

	flag.StringVar(&kubeconfig, "kubeconfig", getDefaultKubeconfigPath(), "path to the kubeconfig file")
	flag.StringVar(&namespace, "namespace", "", "namespace to clean up secrets")

	flag.Parse()

	if namespace == "" {
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
		fmt.Printf("Error creating Kubernetes client: %v\n", err)
		os.Exit(1)
	}

	secrets, err := clientset.CoreV1().Secrets(namespace).List(context.TODO(), metav1.ListOptions{})
	if err != nil {
		fmt.Printf("Error listing secrets: %v\n", err)
		os.Exit(1)
	}

	for _, secret := range secrets.Items {
		pods, err := clientset.CoreV1().Pods(namespace).List(context.TODO(), metav1.ListOptions{
			LabelSelector: fmt.Sprintf("secret=%s", secret.Name),
		})
		if err != nil {
			fmt.Printf("Error listing pods: %v\n", err)
			continue
		}

		if len(pods.Items) == 0 {
			fmt.Printf("Deleting secret %s as it is not used by any pods\n", secret.Name)
			err := clientset.CoreV1().Secrets(namespace).Delete(context.TODO(), secret.Name, metav1.DeleteOptions{})
			if err != nil {
				fmt.Printf("Error deleting secret %s: %v\n", secret.Name, err)
			}
		}
	}

	fmt.Println("Cleanup completed.")
}

func getDefaultKubeconfigPath() string {
	home := homedir.HomeDir()
	return filepath.Join(home, ".kube", "config")
}
