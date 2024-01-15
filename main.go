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

	if err := cleanupSecrets(clientset, namespace); err != nil {
		fmt.Printf("Error cleaning up secrets: %v\n", err)
		os.Exit(1)
	}

	fmt.Println("Cleanup completed.")
}

func cleanupSecrets(clientset *kubernetes.Clientset, namespace string) error {
	secrets, err := clientset.CoreV1().Secrets(namespace).List(context.TODO(), metav1.ListOptions{})
	if err != nil {
		return fmt.Errorf("error listing secrets: %v", err)
	}

	for _, secret := range secrets.Items {
		if strings.HasSuffix(secret.Name, "-certificate") && isEmptyOwnerReference(secret) && !strings.Contains(secret.Name, "root") {
			fmt.Printf("Deleting secret %s as it is not used by any pods\n", secret.Name)
			if err := clientset.CoreV1().Secrets(namespace).Delete(context.TODO(), secret.Name, metav1.DeleteOptions{}); err != nil {
				fmt.Printf("Error deleting secret %s: %v\n", secret.Name, err)
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
