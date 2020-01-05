package main

import (
	"fmt"
	"strconv"
	"strings"

	log "github.com/sirupsen/logrus"
	"istio.io/api/networking/v1alpha3"
	versionedclient "istio.io/client-go/pkg/clientset/versioned"
	core_v1 "k8s.io/api/core/v1"
	v1 "k8s.io/api/core/v1"
	meta_v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

// Handler interface contains the methods that are required
type Handler interface {
	Init() error
	ObjectCreated(obj interface{})
	ObjectDeleted(obj interface{})
	ObjectUpdated(objOld, objNew interface{})
	UpdateVirtualService(obj interface{})
}

// VirtualServiceUpdateHandler is a sample implementation of Handler
type VirtualServiceUpdateHandler struct {
	istioClient *versionedclient.Clientset
	clientSet   kubernetes.Interface
}

// Init handles any handler initialization
func (vsu *VirtualServiceUpdateHandler) Init() error {
	log.Info("VirtualServiceUpdateHandler.Init")
	return nil
}

// ObjectCreated is called when an object is created
func (vsu *VirtualServiceUpdateHandler) ObjectCreated(obj interface{}) {
	log.Info("VirtualServiceUpdateHandler.ObjectCreated")
}

// ObjectDeleted is called when an object is deleted
func (vsu *VirtualServiceUpdateHandler) ObjectDeleted(obj interface{}) {
	log.Info("VirtualServiceUpdateHandler.ObjectDeleted")
}

// ObjectUpdated is called when an object is updated.
// Note that the controller in this repo will never call this function properly.
// It uses only ObjectCreated
func (vsu *VirtualServiceUpdateHandler) ObjectUpdated(objOld, objNew interface{}) {
	log.Info("VirtualServiceUpdateHandler.ObjectUpdated")
}

// UpdateVirtualService is called when a service with specific label is changed
func (vsu *VirtualServiceUpdateHandler) UpdateVirtualService(obj interface{}) {
	log.Info("VirtualServiceUpdateHandler.UpdateVirtualService")

	// The label value uses the format <namespace>.<name>
	labelValue := obj.(*core_v1.Service).Labels[serviceLabel]
	vsMetadata := strings.Split(labelValue, ".")
	vsNamespace := vsMetadata[0]
	vsName := vsMetadata[1]

	serviceList, _ := getServices(labelValue, vsu.clientSet)
	routes := make([]*v1alpha3.HTTPRoute, len(serviceList.Items))

	for index, service := range serviceList.Items {

		prefix := "/" + service.GetNamespace() + "/" + service.GetName()
		host := service.GetName() + "." + service.GetNamespace() + ".svc.cluster.local"

		port, err := getServicePort(service)
		if err != nil {
			log.Errorf("Failed to get port from service: %s", err)
			return
		}

		newRoute := newHTTPRoute(prefix, host, port)

		routes[index] = &newRoute
	}

	vs, err := vsu.istioClient.NetworkingV1alpha3().VirtualServices(vsNamespace).Get(vsName, metav1.GetOptions{})
	if err != nil {
		log.Errorf("Failed to get %s virtual service in namespace %s: %s", vsName, vsNamespace, err)
	}

	vs.Spec.Http = routes

	_, updateErr := vsu.istioClient.NetworkingV1alpha3().VirtualServices(vsNamespace).Update(vs)

	if updateErr != nil {
		log.Errorf("Failed to update %s VirtualService in %s namespace: %s", vsName, vsNamespace, err)
	}
}

func getServices(serviceLabelValue string, client kubernetes.Interface) (*v1.ServiceList, error) {
	labelSelector := fmt.Sprintf("%s=%s", serviceLabel, serviceLabelValue)

	options := meta_v1.ListOptions{
		LabelSelector: labelSelector,
	}

	return client.CoreV1().Services(meta_v1.NamespaceAll).List(options)
}

func getServicePort(service v1.Service) (int32, error) {
	// If the service port annotation exists then route to this port else use the first service port
	if value, exists := service.Annotations[serviceAnnotation]; exists {
		portInt, err := strconv.Atoi(value)
		if err != nil {
			return 0, err
		}

		return int32(portInt), nil
	}

	return service.Spec.Ports[0].Port, nil
}

func newHTTPRoute(prefix, host string, port int32) v1alpha3.HTTPRoute {
	return v1alpha3.HTTPRoute{
		Match: []*v1alpha3.HTTPMatchRequest{
			{
				Uri: &v1alpha3.StringMatch{
					MatchType: &v1alpha3.StringMatch_Prefix{
						Prefix: prefix,
					},
				},
			},
		},
		Headers: &v1alpha3.Headers{
			Response: &v1alpha3.Headers_HeaderOperations{
				Remove: []string{
					"x-envoy-upstream-healthchecked-cluster",
					"x-envoy-upstream-service-time",
				},
			},
		},
		Rewrite: &v1alpha3.HTTPRewrite{
			Uri: "/",
		},
		Route: []*v1alpha3.HTTPRouteDestination{
			{
				Destination: &v1alpha3.Destination{
					Host: host,
					Port: &v1alpha3.PortSelector{
						Number: uint32(port),
					},
				},
			},
		},
	}
}
