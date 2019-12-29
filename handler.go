package main

import (
	"strconv"
	"strings"

	log "github.com/sirupsen/logrus"
	"istio.io/api/networking/v1alpha3"
	versionedclient "istio.io/client-go/pkg/clientset/versioned"
	core_v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// Handler interface contains the methods that are required
type Handler interface {
	Init() error
	ObjectCreated(obj interface{})
	ObjectDeleted(obj interface{})
	ObjectUpdated(objOld, objNew interface{})
}

// VirtualServiceUpdateHandler is a sample implementation of Handler
type VirtualServiceUpdateHandler struct {
	istioClient *versionedclient.Clientset
}

// Init handles any handler initialization
func (vsu *VirtualServiceUpdateHandler) Init() error {
	log.Info("VirtualServiceUpdateHandler.Init")
	return nil
}

// ObjectCreated is called when an object is created
func (vsu *VirtualServiceUpdateHandler) ObjectCreated(obj interface{}) {
	log.Info("VirtualServiceUpdateHandler.ObjectCreated")

	var port int32

	service := obj.(*core_v1.Service)
	prefix := "/" + service.GetNamespace() + "/" + service.GetName()
	host := service.GetName() + "." + service.GetNamespace() + ".svc.cluster.local"

	// The label value uses the format <namespace>.<name>
	labelValue := strings.Split(service.Labels[serviceLabel], ".")
	namespace := labelValue[0]
	vsName := labelValue[1]

	// If the service port annotation exists then route to this port else use the first service port
	if value, exists := service.Annotations[serviceAnnotation]; exists {
		i, err := strconv.Atoi(value)
		if err != nil {
			log.Errorf("Failed to get port number from service annotation: %s", err)
			return
		}

		port = int32(i)
	} else {
		port = service.Spec.Ports[0].Port
	}

	vs, err := vsu.istioClient.NetworkingV1alpha3().VirtualServices(namespace).Get(vsName, metav1.GetOptions{})
	if err != nil {
		log.Errorf("Failed to get %s virtual service in namespace %s: %s", vsName, namespace, err)
	}

	if exists, _ := isPrefixExist(vs.Spec.GetHttp(), prefix); !exists {
		newRoute := newHTTPRoute(prefix, host, port)
		httpRoutes := append(vs.Spec.GetHttp(), &newRoute)
		vs.Spec.Http = httpRoutes

		_, updateErr := vsu.istioClient.NetworkingV1alpha3().VirtualServices(namespace).Update(vs)

		if updateErr != nil {
			log.Errorf("Failed to update VirtualService in %s namespace: %s", namespace, err)
		}
	}
}

// ObjectDeleted is called when an object is deleted
func (vsu *VirtualServiceUpdateHandler) ObjectDeleted(obj interface{}) {
	log.Info("VirtualServiceUpdateHandler.ObjectDeleted")

	service := obj.(*core_v1.Service)
	prefix := "/" + service.GetNamespace() + "/" + service.GetName()

	// The label value uses the format <namespace>.<name>
	labelValue := strings.Split(service.Labels[serviceLabel], ".")
	namespace := labelValue[0]
	vsName := labelValue[1]

	vs, err := vsu.istioClient.NetworkingV1alpha3().VirtualServices(namespace).Get(vsName, metav1.GetOptions{})
	if err != nil {
		log.Errorf("Failed to get %s virtual service in namespace %s: %s", vsName, namespace, err)
	}

	if exists, index := isPrefixExist(vs.Spec.GetHttp(), prefix); exists {
		httpRoutes := vs.Spec.GetHttp()
		httpRoutes = append(httpRoutes[:index], httpRoutes[index+1:]...)
		vs.Spec.Http = httpRoutes

		_, updateErr := vsu.istioClient.NetworkingV1alpha3().VirtualServices(namespace).Update(vs)

		if updateErr != nil {
			log.Errorf("Failed to update VirtualService in %s namespace: %s", namespace, err)
		}
	}
}

// ObjectUpdated is called when an object is updated.
// Note that the controller in this repo will never call this function properly.
// It uses only ObjectCreated
func (vsu *VirtualServiceUpdateHandler) ObjectUpdated(objOld, objNew interface{}) {
	log.Info("VirtualServiceUpdateHandler.ObjectUpdated")
}

// isPrefixExist is called for checking if prefix is existing in a given HTTPRoutes
func isPrefixExist(httpRoutes []*v1alpha3.HTTPRoute, prefix string) (bool, int) {
	for index, route := range httpRoutes {
		if route.GetMatch()[0].GetUri().GetPrefix() == prefix {
			return true, index
		}
	}

	return false, -1
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
