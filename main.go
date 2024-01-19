package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
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
	if namespace == "" && !allNamespaces {
		fmt.Println("Please specify the namespace using the -namespace flag.")
		os.Exit(1)
	}

	var clientset *kubernetes.Clientset

	// Check if running inside a Kubernetes cluster
	if _, err := os.Stat("/var/run/secrets/kubernetes.io/serviceaccount/token"); err == nil {
		// Running inside a Kubernetes cluster, use in-cluster configuration
		config, err := rest.InClusterConfig()
		if err != nil {
			fmt.Printf("Error building in-cluster kubeconfig: %v\n", err)
			os.Exit(1)
		}
		// Use the config to create a Kubernetes client
		clientset, err = kubernetes.NewForConfig(config)
		if err != nil {
			fmt.Printf("Error creating Kubernetes client: %v\n", err)
			os.Exit(1)
		}
	} else {
		// Running outside a Kubernetes cluster, use kubeconfig file
		kubeconfig := filepath.Join(
			os.Getenv("HOME"), ".kube", "config",
		)
		config, err := clientcmd.BuildConfigFromFlags("", kubeconfig)
		if err != nil {
			fmt.Printf("Error building kubeconfig: %v\n", err)
			os.Exit(1)
		}
		// Use the config to create a Kubernetes client
		clientset, err = kubernetes.NewForConfig(config)
		if err != nil {
			fmt.Printf("Error creating Kubernetes client: %v\n", err)
			os.Exit(1)
		}
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
		fmt.Printf("processing namespace %s\n", namespace.Name)
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

	// Use a channel to communicate between goroutines
	secretChan := make(chan v1.Secret)
	errChan := make(chan error)

	// Use a WaitGroup to wait for all goroutines to finish
	var wg sync.WaitGroup

	// Launch 10 goroutines
	numWorkers := 5
	for i := 0; i < numWorkers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for secret := range secretChan {
				if strings.HasSuffix(secret.Name, "-certificate") && isEmptyOwnerReference(secret) && !strings.Contains(secret.Name, "root") {
					fmt.Printf("Deleting secret %s as it is not used by any pods\n", secret.Name)
					if !dryRun {
						if err := clientset.CoreV1().Secrets(namespace).Delete(context.TODO(), secret.Name, metav1.DeleteOptions{}); err != nil {
							errChan <- fmt.Errorf("Error deleting secret %s: %v\n", secret.Name, err)
						}
					}
				}
			}
			pods, err := clientset.CoreV1().Pods(namespace).List(context.TODO(), metav1.ListOptions{})
			if err != nil {
				errChan <- fmt.Errorf("Error listing pods: %v\n", err)
				return
			}

			// Extract the first part of the pod name
			var podPrefixes []string
			for _, pod := range pods.Items {
				parts := strings.Split(pod.Name, "-an-")
				if len(parts) == 2 && len(parts[0]) == 10 {
					podPrefixes = append(podPrefixes, parts[0])
				}
			}

			// List all services in the namespace
			services, err := clientset.CoreV1().Services(namespace).List(context.TODO(), metav1.ListOptions{})
			if err != nil {
				errChan <- fmt.Errorf("Error listing services: %v\n", err)
				return
			}

			// Delete services that don't have the first part of the pod name in their name
			for _, service := range services.Items {
				shouldDelete := true
				for _, prefix := range podPrefixes {
					if !strings.Contains(service.Name, "an-config") {
						shouldDelete = false
						break
					}
					if strings.Contains(service.Name, prefix) {
						shouldDelete = false
						break
					}
				}

				if shouldDelete {
					fmt.Printf("Deleting service %s as it is not associated with any relevant pods\n", service.Name)
					if !dryRun {
						if err := clientset.CoreV1().Services(namespace).Delete(context.TODO(), service.Name, metav1.DeleteOptions{}); err != nil {
							errChan <- fmt.Errorf("Error deleting service %s: %v\n", service.Name, err)
						}
					}
				}
			}

		}()
	}

	// Send secrets to the channel
	go func() {
		defer close(secretChan)
		for _, secret := range secrets.Items {
			secretChan <- secret
		}
	}()

	// Wait for all goroutines to finish
	go func() {
		wg.Wait()
		close(errChan)
	}()

	// Collect errors from goroutines
	for err := range errChan {
		if err != nil {
			return err
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
