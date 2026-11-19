package tls

import (
	"crypto/tls"
	"testing"

	"github.com/go-logr/logr"
	"github.com/onsi/gomega"
	configv1 "github.com/openshift/api/config/v1"
	"github.com/openshift/lvm-operator/v4/internal/cluster"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestResolveTLSProfile(test *testing.T) {
	for _, testCase := range []struct {
		name       string
		minVersion configv1.TLSProtocolVersion
		wantError  bool
	}{
		{name: "valid minimum TLS version", minVersion: configv1.VersionTLS12},
		{name: "invalid minimum TLS version", minVersion: "invalid", wantError: true},
	} {
		test.Run(testCase.name, func(test *testing.T) {
			assert := gomega.NewWithT(test)
			scheme := runtime.NewScheme()
			assert.Expect(configv1.AddToScheme(scheme)).To(gomega.Succeed())
			apiServer := &configv1.APIServer{
				ObjectMeta: metav1.ObjectMeta{Name: "cluster"},
				Spec: configv1.APIServerSpec{
					TLSSecurityProfile: &configv1.TLSSecurityProfile{
						Type: configv1.TLSProfileCustomType,
						Custom: &configv1.CustomTLSProfile{
							TLSProfileSpec: configv1.TLSProfileSpec{MinTLSVersion: testCase.minVersion},
						},
					},
				},
			}
			setupClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(apiServer).Build()
			setup, err := Resolve(test.Context(), cluster.TypeOCP, setupClient, logr.Discard())
			if testCase.wantError {
				assert.Expect(err).To(gomega.MatchError(gomega.ContainSubstring("failed to configure tls profile: invalid minimum TLS version")))
				assert.Expect(setup).To(gomega.BeNil())
				return
			}
			assert.Expect(err).NotTo(gomega.HaveOccurred())
			tlsConfig := &tls.Config{}
			for _, option := range setup.Options() {
				option(tlsConfig)
			}
			assert.Expect(tlsConfig.MinVersion).To(gomega.Equal(uint16(tls.VersionTLS12)))
			assert.Expect(tlsConfig.NextProtos).To(gomega.Equal([]string{"http/1.1"}))
		})
	}
}
