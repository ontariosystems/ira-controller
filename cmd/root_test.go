package cmd

import (
	"flag"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/gbytes"
	v1 "github.com/ontariosystems/ira-controller/api/v1"
	"github.com/ontariosystems/ira-controller/internal/controller"
)

var _ = Describe("Cmd configure", func() {
	var buffer *gbytes.Buffer
	BeforeEach(func() {
		buffer = gbytes.NewBuffer()
		GinkgoWriter.TeeTo(buffer)
	})
	AfterEach(func() {
		GinkgoWriter.ClearTeeWriters()
		v1.CredentialHelperImage = ""
		controller.DefaultIssuerKind = ""
	})
	Context("When configuring the root command", func() {
		Context("without an image provided", func() {
			It("should return an error", func() {
				mgr, rc := configure(&rootFlags{metricsAddr: "0", probeAddr: ":8081"})
				Expect(mgr).To(BeNil())
				Expect(rc).To(Equal(1))
			})
		})
		Context("with an image provided", func() {
			BeforeEach(func() {
				v1.CredentialHelperImage = "test:image"
			})
			Context("with an invalid issuer kind", func() {
				It("should return an error", func() {
					mgr, rc := configure(&rootFlags{metricsAddr: "0", probeAddr: ":8081"})
					Expect(mgr).To(BeNil())
					Expect(rc).To(Equal(1))
				})
			})
			Context("with a valid issuer kind", func() {
				BeforeEach(func() {
					controller.DefaultIssuerKind = "ClusterIssuer"
				})
				It("should return the manager", func() {
					mgr, rc := configure(&rootFlags{generateCert: true, metricsAddr: "0", probeAddr: ":0"})
					Expect(mgr).ToNot(BeNil())
					Expect(rc).To(Equal(0))
				})
				Context("when provided an invalid health probe address", func() {
					It("should log a message", func() {
						mgr, rc := configure(&rootFlags{metricsAddr: "0", probeAddr: ":100000"})
						Expect(mgr).To(BeNil())
						Expect(rc).To(Equal(1))

						Eventually(func() *gbytes.Buffer {
							return buffer
						}, 10*time.Second, 25*time.Millisecond).Should(gbytes.Say("unable to start manager"))
					})
				})
			})
		})
	})
	Context("When adding flags", func() {
		It("should add the flags", func() {
			f := addFlags()
			Expect(f).ToNot(BeNil())
			Expect(flag.Lookup("metrics-bind-address")).To(HaveField("DefValue", "0"))
			Expect(flag.Lookup("health-probe-bind-address")).To(HaveField("DefValue", ":8081"))
			Expect(flag.Lookup("leader-elect")).To(HaveField("DefValue", "false"))
			Expect(flag.Lookup("metrics-secure")).To(HaveField("DefValue", "false"))
			Expect(flag.Lookup("enable-http2")).To(HaveField("DefValue", "false"))
			Expect(flag.Lookup("generate-cert")).To(HaveField("DefValue", "false"))
			Expect(flag.Lookup("credential-helper-image")).To(HaveField("DefValue", ""))
			Expect(flag.Lookup("credential-helper-cpu-request")).To(HaveField("DefValue", "250m"))
			Expect(flag.Lookup("credential-helper-memory-request")).To(HaveField("DefValue", "64Mi"))
			Expect(flag.Lookup("credential-helper-cpu-limit")).To(HaveField("DefValue", ""))
			Expect(flag.Lookup("credential-helper-memory-limit")).To(HaveField("DefValue", "128Mi"))
			Expect(flag.Lookup("credential-helper-session-duration")).To(HaveField("DefValue", "900"))
			Expect(flag.Lookup("default-issuer-kind")).To(HaveField("DefValue", "ClusterIssuer"))
			Expect(flag.Lookup("default-issuer-name")).To(HaveField("DefValue", ""))
		})
	})
})
