package lvmd

import (
	"context"
	"fmt"

	"github.com/openshift/lvm-operator/internal/controllers/constants"
	"github.com/topolvm/topolvm/lvmd"
	lvmdCMD "github.com/topolvm/topolvm/pkg/lvmd/cmd"

	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/yaml"
)

type Config = lvmdCMD.Config

type DeviceClass = lvmd.DeviceClass
type ThinPoolConfig = lvmd.ThinPoolConfig

var TypeThin = lvmd.TypeThin

func NewFileConfigurator(client client.Client, namespace string) *CachedFileConfig {
	return &CachedFileConfig{
		FileConfig: FileConfig{Client: client, Namespace: namespace},
	}
}

type Configurator interface {
	Load(ctx context.Context) (*Config, error)
	Save(ctx context.Context, config *Config) error
	Delete(ctx context.Context) error
}

type CachedFileConfig struct {
	*Config
	FileConfig
}

func (c *CachedFileConfig) Load(ctx context.Context) (*Config, error) {
	if c.Config != nil {
		return c.Config, nil
	}
	log.FromContext(ctx).Info("lvmd config not found in cache, loading from store")
	conf, err := c.FileConfig.Load(ctx)
	if err != nil {
		return nil, err
	}
	c.Config = conf
	return conf, nil
}

func (c *CachedFileConfig) Save(ctx context.Context, config *Config) error {
	c.Config = config
	log.FromContext(ctx).Info("saving lvmd config to cache and store")
	return c.FileConfig.Save(ctx, config)
}

type FileConfig struct {
	client.Client
	Namespace string
}

func (c FileConfig) Load(ctx context.Context) (*Config, error) {
	cm := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      constants.LVMDConfigMapName,
			Namespace: c.Namespace,
		},
	}
	err := c.Client.Get(ctx, client.ObjectKeyFromObject(cm), cm)
	if k8serrors.IsNotFound(err) {
		// If the file does not exist, return nil for both
		return nil, nil
	} else if err != nil {
		return nil, fmt.Errorf("failed to get ConfigMap %s: %w", cm.Name, err)
	}

	config := &Config{}
	if err = yaml.Unmarshal([]byte(cm.Data["lvmd.yaml"]), config); err != nil {
		return nil, fmt.Errorf("failed to unmarshal config file: %w", err)
	}
	return config, nil
}

func (c FileConfig) Save(ctx context.Context, config *Config) error {
	out, err := yaml.Marshal(config)
	if err != nil {
		return fmt.Errorf("failed to marshal config file: %w", err)
	}

	cm := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      constants.LVMDConfigMapName,
			Namespace: c.Namespace,
		},
	}
	_, err = ctrl.CreateOrUpdate(ctx, c.Client, cm, func() error {
		cm.Data = map[string]string{"lvmd.yaml": string(out)}
		return nil
	})
	if err != nil {
		return fmt.Errorf("failed to apply ConfigMap %s: %w", cm.GetName(), err)
	}

	return nil
}

func (c FileConfig) Delete(ctx context.Context) error {
	cm := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      constants.LVMDConfigMapName,
			Namespace: c.Namespace,
		},
	}
	if err := c.Client.Delete(ctx, cm); err != nil {
		if k8serrors.IsNotFound(err) {
			// If the file does not exist, return nil
			return nil
		}
		return fmt.Errorf("failed to delete ConfigMap: %w", err)
	}

	return nil
}
