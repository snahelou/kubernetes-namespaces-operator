package controller

import (
	"fmt"
	"k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/cache"
	"log"
	"regexp"
	"sync"
	"time"
)

// NamespaceController watches the kubernetes api for changes to namespaces and
// creates a RoleBinding for that particular namespace.
type NamespaceController struct {
	namespaceInformer cache.SharedIndexInformer
	kclient           *kubernetes.Clientset
}

// Run starts the process for listening for namespace changes and acting upon those changes.
func (c *NamespaceController) Run(stopCh <-chan struct{}, wg *sync.WaitGroup) {
	// When this function completes, mark the go function as done
	defer wg.Done()

	// Increment wait group as we're about to execute a go function
	wg.Add(1)

	// Execute go function
	go c.namespaceInformer.Run(stopCh)

	// Wait till we receive a stop signal
	<-stopCh
}

// NewNamespaceController creates a new NewNamespaceController
func NewNamespaceController(kclient *kubernetes.Clientset) *NamespaceController {
	namespaceWatcher := &NamespaceController{}

	// Create informer for watching Namespaces
	namespaceInformer := cache.NewSharedIndexInformer(
		&cache.ListWatch{
			ListFunc: func(options metav1.ListOptions) (runtime.Object, error) {
				return kclient.CoreV1().Namespaces().List(options)
			},
			WatchFunc: func(options metav1.ListOptions) (watch.Interface, error) {
				return kclient.CoreV1().Namespaces().Watch(options)
			},
		},
		&v1.Namespace{},
		3*time.Minute,
		cache.Indexers{cache.NamespaceIndex: cache.MetaNamespaceIndexFunc},
	)

	namespaceInformer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: namespaceWatcher.createCustomRules,
	})

	namespaceWatcher.kclient = kclient
	namespaceWatcher.namespaceInformer = namespaceInformer

	return namespaceWatcher
}

func (c *NamespaceController) createCustomRules(obj interface{}) {

	namespaceObj := obj.(*v1.Namespace)
	namespaceName := namespaceObj.Name

	adminNamespaces, _ := regexp.MatchString("kube-.*", namespaceName) // kube-system - kube-public

	limitRangeName := fmt.Sprintf("lr-auto-%s", namespaceName)

	if adminNamespaces != true {

		// Add limitRange for memory
		limit := &v1.LimitRange{
			TypeMeta: metav1.TypeMeta{
				Kind:       "LimitRange",
				APIVersion: "v1",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:      limitRangeName,
				Namespace: namespaceName,
			},

			Spec: v1.LimitRangeSpec{
				Limits: []v1.LimitRangeItem{{
					Type: v1.LimitTypeContainer,
					Default: v1.ResourceList{
						"memory": *resource.NewQuantity(128*1024*1024, resource.BinarySI),
					},
					DefaultRequest: v1.ResourceList{
						"memory": *resource.NewQuantity(128*1024*1024, resource.BinarySI),
					},
				}},
			},
		}

		_, err := c.kclient.CoreV1().LimitRanges(namespaceName).Create(limit)

		if err != nil {
			log.Println(fmt.Sprintf("Failed to create limitRange: %s", err.Error()))
		} else {
			log.Println(fmt.Sprintf("limitRange for Namespace: %s created", namespaceName))
		}


		// Add ResourceQuota for service.loadbalancer & service.nodeport
		resourceQuotaName := fmt.Sprintf("rq-auto-%s", namespaceName)

		quota := &v1.ResourceQuota{
			TypeMeta: metav1.TypeMeta{
				Kind:       "ResourceQuota",
				APIVersion: "v1",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:      resourceQuotaName,
				Namespace: namespaceName,
			},
			Spec: v1.ResourceQuotaSpec{
				Hard: v1.ResourceList{
					"services.loadbalancers": *resource.NewQuantity(0,resource.BinarySI),
					"services.nodeports": *resource.NewQuantity(0,resource.BinarySI),
				},
			},
		}

		_, err = c.kclient.CoreV1().ResourceQuotas(namespaceName).Create(quota)

		if err != nil {
			log.Println(fmt.Sprintf("Failed to create ResourceQuotas: %s", err.Error()))
		} else {
			log.Println(fmt.Sprintf("ResourceQuotas for Namespace: %s created", namespaceName))
		}


	} else {
		log.Println(fmt.Sprintf("Skip admin namespace: %s", namespaceName))
	}
}
