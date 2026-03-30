package main

import (
	"context"
	"log"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

// K8sPoller polls the Kubernetes API for Deployment and StatefulSet replica
// counts every pollInterval and broadcasts "pods" events via the Hub.
type K8sPoller struct {
	client    kubernetes.Interface
	hub       *Hub
	namespace string
}

func NewK8sPoller(hub *Hub, namespace string) (*K8sPoller, error) {
	cfg, err := rest.InClusterConfig()
	if err != nil {
		return nil, err
	}
	client, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		return nil, err
	}
	return &K8sPoller{client: client, hub: hub, namespace: namespace}, nil
}

// Run blocks and polls K8s every 5 s. Call in a goroutine.
func (p *K8sPoller) Run(ctx context.Context) {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	// Emit once immediately on start.
	p.emit(ctx)

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			p.emit(ctx)
		}
	}
}

func (p *K8sPoller) emit(ctx context.Context) {
	pods := make(PodsPayload)

	// Deployments.
	deployments, err := p.client.AppsV1().Deployments(p.namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		log.Printf("k8s poller: list deployments: %v", err)
	} else {
		for _, d := range deployments.Items {
			pods[d.Name] = PodInfo{
				Desired: *d.Spec.Replicas,
				Ready:   d.Status.ReadyReplicas,
			}
		}
	}

	// StatefulSets (covers Atomix consensus storage).
	statefulSets, err := p.client.AppsV1().StatefulSets(p.namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		log.Printf("k8s poller: list statefulsets: %v", err)
	} else {
		for _, ss := range statefulSets.Items {
			pods[ss.Name] = PodInfo{
				Desired: *ss.Spec.Replicas,
				Ready:   ss.Status.ReadyReplicas,
			}
		}
	}

	if len(pods) == 0 {
		return
	}

	if ev, err := newEvent("pods", "", "", pods); err == nil {
		p.hub.Broadcast(ev)
	}
}
