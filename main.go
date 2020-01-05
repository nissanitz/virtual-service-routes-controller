package main

import (
	"os"
	"os/signal"
	"syscall"

	log "github.com/sirupsen/logrus"
	versionedclient "istio.io/client-go/pkg/clientset/versioned"
	api_v1 "k8s.io/api/core/v1"
	meta_v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/util/workqueue"
)

var (
	// The label value uses the format <namespace>.<name>
	serviceLabel string = "virtualservice.httproute/VirtualServiceName"

	serviceAnnotation string = "virtualservice.httproute/PortNumber"
)

// retrieve the Kubernetes cluster client from outside of the cluster
func getKubernetesClients() (*kubernetes.Clientset, *versionedclient.Clientset) {
	// construct the path to resolve to `~/.kube/config`
	kubeConfigPath := "" // os.Getenv("HOME") + "/.kube/config"

	// create the config from the path
	config, err := clientcmd.BuildConfigFromFlags("", kubeConfigPath)
	if err != nil {
		log.Fatalf("getClusterConfig: %v", err)
	}

	// generate the client based off of the config
	client, err := kubernetes.NewForConfig(config)
	if err != nil {
		log.Fatalf("Failed to create kubernetes client: %v", err)
	}

	istioClient, err := versionedclient.NewForConfig(config)
	if err != nil {
		log.Fatalf("Failed to create istio client: %v", err)
	}

	log.Info("Successfully constructed k8s client")
	return client, istioClient
}

func main() {
	// get the Kubernetes client for connectivity
	client, istioClient := getKubernetesClients()

	namespace := meta_v1.NamespaceAll

	// stored deleted services
	deletedIndexer := cache.NewIndexer(cache.DeletionHandlingMetaNamespaceKeyFunc, cache.Indexers{})

	// create a new queue so that when the informer gets a resource that is either
	// a result of listing or watching, we can add an idenfitying key to the queue
	// so that it can be handled in the handler
	queue := workqueue.NewRateLimitingQueue(workqueue.DefaultControllerRateLimiter())

	// create the informer so that we can not only list resources
	// but also watch them for all services in all namespaces
	informer := cache.NewSharedIndexInformer(
		// the ListWatch contains two different functions that our
		// informer requires: ListFunc to take care of listing and watching
		// the resources we want to handle
		&cache.ListWatch{
			ListFunc: func(options meta_v1.ListOptions) (runtime.Object, error) {
				options.LabelSelector = serviceLabel
				// list all of the services (core resource)
				return client.CoreV1().Services(namespace).List(options)
			},
			WatchFunc: func(options meta_v1.ListOptions) (watch.Interface, error) {
				options.LabelSelector = serviceLabel
				// watch all of the services (core resource)
				return client.CoreV1().Services(namespace).Watch(options)
			},
		},
		&api_v1.Service{}, // the target type (Service)
		0,                 // no resync (period of 0)
		cache.Indexers{},
	)

	// add event handlers to handle the three types of events for resources:
	//  - adding new resources
	//  - updating existing resources
	//  - deleting resources
	informer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj interface{}) {
			// convert the resource object into a key (in this case
			// we are just doing it in the format of 'namespace/name')
			key, err := cache.MetaNamespaceKeyFunc(obj)
			log.Infof("Add service: %s", key)
			if err == nil {
				// add the key to the queue for the handler to get
				queue.Add(key)
				deletedIndexer.Delete(obj)
				log.Infof("  Queue len: %d", queue.Len())
			}
		},
		UpdateFunc: func(oldObj, newObj interface{}) {
			key, err := cache.MetaNamespaceKeyFunc(newObj)
			log.Infof("Update service: %s", key)
			if err == nil {
				queue.Add(key)
				log.Infof("  Queue len: %d", queue.Len())
			}
		},
		DeleteFunc: func(obj interface{}) {
			// DeletionHandlingMetaNamsespaceKeyFunc is a helper function that allows
			// us to check the DeletedFinalStateUnknown existence in the event that
			// a resource was deleted but it is still contained in the index
			//
			// this then in turn calls MetaNamespaceKeyFunc
			key, err := cache.DeletionHandlingMetaNamespaceKeyFunc(obj)
			log.Infof("Delete service: %s", key)
			if err == nil {
				queue.Add(key)
				deletedIndexer.Add(obj)
				log.Infof("  Queue len: %d", queue.Len())
			}
		},
	})

	// construct the Controller object which has all of the necessary components to
	// handle logging, connections, informing (listing and watching) and deleted indexer, the queue,
	// and the handler
	controller := NewController(queue, informer, deletedIndexer, client, istioClient)

	// use a channel to synchronize the finalization for a graceful shutdown
	stopCh := make(chan struct{})
	defer close(stopCh)

	// run the controller loop to process items
	go controller.Run(stopCh)

	// use a channel to handle OS signals to terminate and gracefully shut
	// down processing
	sigTerm := make(chan os.Signal, 1)
	signal.Notify(sigTerm, syscall.SIGTERM)
	signal.Notify(sigTerm, syscall.SIGINT)
	<-sigTerm

	log.Info("Shutting down....")

}
