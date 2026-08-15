package kubernetes

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/tools/clientcmd/api"
)

// clientPool caches K8s clients keyed by kubeconfig context.
var (
	clientMu    sync.Mutex
	clientPool  = map[string]*kubernetes.Clientset{}
	configCache *api.Config
)

// loadConfig reads the kubeconfig once and caches it.
func loadConfig() (*api.Config, error) {
	clientMu.Lock()
	defer clientMu.Unlock()
	if configCache != nil {
		return configCache, nil
	}
	kubeconfig := os.Getenv("KUBECONFIG")
	if kubeconfig == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return nil, fmt.Errorf("home dir: %w", err)
		}
		kubeconfig = filepath.Join(home, ".kube", "config")
	}
	config, err := clientcmd.LoadFromFile(kubeconfig)
	if err != nil {
		return nil, fmt.Errorf("load kubeconfig %s: %w", kubeconfig, err)
	}
	configCache = config
	return config, nil
}

// Contexts returns all available kubeconfig context names.
func Contexts() ([]string, error) {
	cfg, err := loadConfig()
	if err != nil {
		return nil, err
	}
	names := make([]string, 0, len(cfg.Contexts))
	for name := range cfg.Contexts {
		names = append(names, name)
	}
	return names, nil
}

// clientFor returns a kubernetes.Clientset for the given context.
func clientFor(context string) (*kubernetes.Clientset, error) {
	clientMu.Lock()
	defer clientMu.Unlock()
	if c, ok := clientPool[context]; ok {
		return c, nil
	}
	kubeconfig := os.Getenv("KUBECONFIG")
	if kubeconfig == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return nil, fmt.Errorf("home dir: %w", err)
		}
		kubeconfig = filepath.Join(home, ".kube", "config")
	}
	config, err := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(
		&clientcmd.ClientConfigLoadingRules{ExplicitPath: kubeconfig},
		&clientcmd.ConfigOverrides{CurrentContext: context},
	).ClientConfig()
	if err != nil {
		return nil, fmt.Errorf("build config for context %q: %w", context, err)
	}
	cs, err := kubernetes.NewForConfig(config)
	if err != nil {
		return nil, fmt.Errorf("create client for context %q: %w", context, err)
	}
	clientPool[context] = cs
	return cs, nil
}

// CurrentContext returns the current kubeconfig context name.
func CurrentContext() (string, error) {
	cfg, err := loadConfig()
	if err != nil {
		return "", err
	}
	if cfg.CurrentContext != "" {
		return cfg.CurrentContext, nil
	}
	return "", fmt.Errorf("no current context set in kubeconfig")
}
