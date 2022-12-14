package state_test

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/gateway-api/apis/v1alpha2"
	"sigs.k8s.io/gateway-api/apis/v1beta1"

	"github.com/nginxinc/nginx-kubernetes-gateway/internal/helpers"
	"github.com/nginxinc/nginx-kubernetes-gateway/internal/state"
	"github.com/nginxinc/nginx-kubernetes-gateway/internal/state/statefakes"
)

// FIXME(kate-osborn): Consider refactoring these tests to reduce code duplication.
var _ = Describe("ChangeProcessor", func() {
	Describe("Normal cases of processing changes", func() {
		const (
			controllerName  = "my.controller"
			gcName          = "test-class"
			certificatePath = "path/to/cert"
		)

		var (
			gc, gcUpdated        *v1beta1.GatewayClass
			hr1, hr1Updated, hr2 *v1beta1.HTTPRoute
			gw1, gw1Updated, gw2 *v1beta1.Gateway
			processor            state.ChangeProcessor
			fakeSecretMemoryMgr  *statefakes.FakeSecretDiskMemoryManager
		)

		BeforeEach(OncePerOrdered, func() {
			gc = &v1beta1.GatewayClass{
				ObjectMeta: metav1.ObjectMeta{
					Name:       gcName,
					Generation: 1,
				},
				Spec: v1beta1.GatewayClassSpec{
					ControllerName: controllerName,
				},
			}

			gcUpdated = gc.DeepCopy()
			gcUpdated.Generation++

			createRoute := func(name string, gateway string, hostname string) *v1beta1.HTTPRoute {
				return &v1beta1.HTTPRoute{
					ObjectMeta: metav1.ObjectMeta{
						Namespace: "test",
						Name:      name,
					},
					Spec: v1beta1.HTTPRouteSpec{
						CommonRouteSpec: v1beta1.CommonRouteSpec{
							ParentRefs: []v1beta1.ParentReference{
								{
									Namespace:   (*v1beta1.Namespace)(helpers.GetStringPointer("test")),
									Name:        v1beta1.ObjectName(gateway),
									SectionName: (*v1beta1.SectionName)(helpers.GetStringPointer("listener-80-1")),
								},
								{
									Namespace:   (*v1beta1.Namespace)(helpers.GetStringPointer("test")),
									Name:        v1beta1.ObjectName(gateway),
									SectionName: (*v1beta1.SectionName)(helpers.GetStringPointer("listener-443-1")),
								},
							},
						},
						Hostnames: []v1beta1.Hostname{
							v1beta1.Hostname(hostname),
						},
						Rules: []v1beta1.HTTPRouteRule{
							{
								Matches: []v1beta1.HTTPRouteMatch{
									{
										Path: &v1beta1.HTTPPathMatch{
											Value: helpers.GetStringPointer("/"),
										},
									},
								},
							},
						},
					},
				}
			}

			hr1 = createRoute("hr-1", "gateway-1", "foo.example.com")

			hr1Updated = hr1.DeepCopy()
			hr1Updated.Generation++

			hr2 = createRoute("hr-2", "gateway-2", "bar.example.com")

			createGateway := func(name string) *v1beta1.Gateway {
				return &v1beta1.Gateway{
					ObjectMeta: metav1.ObjectMeta{
						Namespace:  "test",
						Name:       name,
						Generation: 1,
					},
					Spec: v1beta1.GatewaySpec{
						GatewayClassName: gcName,
						Listeners: []v1beta1.Listener{
							{
								Name:     "listener-80-1",
								Hostname: nil,
								Port:     80,
								Protocol: v1beta1.HTTPProtocolType,
							},
							{
								Name:     "listener-443-1",
								Hostname: nil,
								Port:     443,
								Protocol: v1beta1.HTTPSProtocolType,
								TLS: &v1beta1.GatewayTLSConfig{
									Mode: helpers.GetTLSModePointer(v1beta1.TLSModeTerminate),
									CertificateRefs: []v1beta1.SecretObjectReference{
										{
											Kind:      (*v1beta1.Kind)(helpers.GetStringPointer("Secret")),
											Name:      "secret",
											Namespace: (*v1beta1.Namespace)(helpers.GetStringPointer("test")),
										},
									},
								},
							},
						},
					},
				}
			}

			gw1 = createGateway("gateway-1")

			gw1Updated = gw1.DeepCopy()
			gw1Updated.Generation++

			gw2 = createGateway("gateway-2")

			fakeSecretMemoryMgr = &statefakes.FakeSecretDiskMemoryManager{}

			processor = state.NewChangeProcessorImpl(state.ChangeProcessorConfig{
				GatewayCtlrName:     controllerName,
				GatewayClassName:    gcName,
				SecretMemoryManager: fakeSecretMemoryMgr,
			})

			fakeSecretMemoryMgr.RequestReturns(certificatePath, nil)
		})

		Describe("Process resources", Ordered, func() {
			When("no upsert has occurred", func() {
				It("should return empty configuration and statuses", func() {
					changed, conf, statuses := processor.Process()
					Expect(changed).To(BeFalse())
					Expect(conf).To(BeZero())
					Expect(statuses).To(BeZero())
				})
			})
			When("GatewayClass doesn't exist", func() {
				When("Gateways don't exist", func() {
					It("should return empty configuration and updated statuses after upserting the first HTTPRoute", func() {
						processor.CaptureUpsertChange(hr1)

						expectedConf := state.Configuration{}
						expectedStatuses := state.Statuses{
							IgnoredGatewayStatuses: map[types.NamespacedName]state.IgnoredGatewayStatus{},
							HTTPRouteStatuses:      map[types.NamespacedName]state.HTTPRouteStatus{},
						}

						changed, conf, statuses := processor.Process()
						Expect(changed).To(BeTrue())
						Expect(helpers.Diff(expectedConf, conf)).To(BeEmpty())
						Expect(helpers.Diff(expectedStatuses, statuses)).To(BeEmpty())
					})
				})

				It("should return empty configuration and updated statuses after upserting the first Gateway", func() {
					processor.CaptureUpsertChange(gw1)

					expectedConf := state.Configuration{}
					expectedStatuses := state.Statuses{
						GatewayStatus: &state.GatewayStatus{
							NsName: types.NamespacedName{Namespace: "test", Name: "gateway-1"},
							ListenerStatuses: map[string]state.ListenerStatus{
								"listener-80-1": {
									Valid:          false,
									AttachedRoutes: 1,
								},
								"listener-443-1": {
									Valid:          false,
									AttachedRoutes: 1,
								},
							},
						},
						IgnoredGatewayStatuses: map[types.NamespacedName]state.IgnoredGatewayStatus{},
						HTTPRouteStatuses: map[types.NamespacedName]state.HTTPRouteStatus{
							{Namespace: "test", Name: "hr-1"}: {
								ParentStatuses: map[string]state.ParentStatus{
									"listener-80-1":  {Attached: false},
									"listener-443-1": {Attached: false},
								},
							},
						},
					}

					changed, conf, statuses := processor.Process()
					Expect(changed).To(BeTrue())
					Expect(helpers.Diff(expectedConf, conf)).To(BeEmpty())
					Expect(helpers.Diff(expectedStatuses, statuses)).To(BeEmpty())
				})
			})

			It("should return updated configuration and statuses after the GatewayClass is upserted", func() {
				processor.CaptureUpsertChange(gc)

				expectedConf := state.Configuration{
					HTTPServers: []state.VirtualServer{
						{
							Hostname: "foo.example.com",
							PathRules: []state.PathRule{
								{
									Path: "/",
									MatchRules: []state.MatchRule{
										{
											MatchIdx: 0,
											RuleIdx:  0,
											Source:   hr1,
										},
									},
								},
							},
						},
					},
					SSLServers: []state.VirtualServer{
						{
							Hostname: "foo.example.com",
							SSL:      &state.SSL{CertificatePath: certificatePath},
							PathRules: []state.PathRule{
								{
									Path: "/",
									MatchRules: []state.MatchRule{
										{
											MatchIdx: 0,
											RuleIdx:  0,
											Source:   hr1,
										},
									},
								},
							},
						},
						{
							Hostname: "~^",
							SSL:      &state.SSL{CertificatePath: certificatePath},
						},
					},
				}

				expectedStatuses := state.Statuses{
					GatewayClassStatus: &state.GatewayClassStatus{
						Valid:              true,
						ObservedGeneration: gc.Generation,
					},
					GatewayStatus: &state.GatewayStatus{
						NsName: types.NamespacedName{Namespace: "test", Name: "gateway-1"},
						ListenerStatuses: map[string]state.ListenerStatus{
							"listener-80-1": {
								Valid:          true,
								AttachedRoutes: 1,
							},
							"listener-443-1": {
								Valid:          true,
								AttachedRoutes: 1,
							},
						},
					},
					IgnoredGatewayStatuses: map[types.NamespacedName]state.IgnoredGatewayStatus{},
					HTTPRouteStatuses: map[types.NamespacedName]state.HTTPRouteStatus{
						{Namespace: "test", Name: "hr-1"}: {
							ParentStatuses: map[string]state.ParentStatus{
								"listener-80-1":  {Attached: true},
								"listener-443-1": {Attached: true},
							},
						},
					},
				}

				changed, conf, statuses := processor.Process()
				Expect(changed).To(BeTrue())
				Expect(helpers.Diff(expectedConf, conf)).To(BeEmpty())
				Expect(helpers.Diff(expectedStatuses, statuses)).To(BeEmpty())
			})

			It("should return empty configuration and statuses after processing upserting the first HTTPRoute without generation change", func() {
				hr1UpdatedSameGen := hr1.DeepCopy()
				// hr1UpdatedSameGen.Generation has not been changed
				processor.CaptureUpsertChange(hr1UpdatedSameGen)

				changed, conf, statuses := processor.Process()
				Expect(changed).To(BeFalse())
				Expect(conf).To(BeZero())
				Expect(statuses).To(BeZero())
			})

			It("should return updated configuration and statuses after upserting the first HTTPRoute with generation change", func() {
				processor.CaptureUpsertChange(hr1Updated)

				expectedConf := state.Configuration{
					HTTPServers: []state.VirtualServer{
						{
							Hostname: "foo.example.com",
							PathRules: []state.PathRule{
								{
									Path: "/",
									MatchRules: []state.MatchRule{
										{
											MatchIdx: 0,
											RuleIdx:  0,
											Source:   hr1Updated,
										},
									},
								},
							},
						},
					},
					SSLServers: []state.VirtualServer{
						{
							Hostname: "foo.example.com",
							SSL:      &state.SSL{CertificatePath: certificatePath},
							PathRules: []state.PathRule{
								{
									Path: "/",
									MatchRules: []state.MatchRule{
										{
											MatchIdx: 0,
											RuleIdx:  0,
											Source:   hr1Updated,
										},
									},
								},
							},
						},
						{
							Hostname: "~^",
							SSL:      &state.SSL{CertificatePath: certificatePath},
						},
					},
				}
				expectedStatuses := state.Statuses{
					GatewayClassStatus: &state.GatewayClassStatus{
						Valid:              true,
						ObservedGeneration: gc.Generation,
					},
					GatewayStatus: &state.GatewayStatus{
						NsName: types.NamespacedName{Namespace: "test", Name: "gateway-1"},
						ListenerStatuses: map[string]state.ListenerStatus{
							"listener-80-1": {
								Valid:          true,
								AttachedRoutes: 1,
							},
							"listener-443-1": {
								Valid:          true,
								AttachedRoutes: 1,
							},
						},
					},
					IgnoredGatewayStatuses: map[types.NamespacedName]state.IgnoredGatewayStatus{},
					HTTPRouteStatuses: map[types.NamespacedName]state.HTTPRouteStatus{
						{Namespace: "test", Name: "hr-1"}: {
							ParentStatuses: map[string]state.ParentStatus{
								"listener-80-1":  {Attached: true},
								"listener-443-1": {Attached: true},
							},
						},
					},
				}

				changed, conf, statuses := processor.Process()
				Expect(changed).To(BeTrue())
				Expect(helpers.Diff(expectedConf, conf)).To(BeEmpty())
				Expect(helpers.Diff(expectedStatuses, statuses)).To(BeEmpty())
			})

			It("should return empty configuration and statuses after processing upserting the first Gateway without generation change", func() {
				gwUpdatedSameGen := gw1.DeepCopy()
				// gwUpdatedSameGen.Generation has not been changed
				processor.CaptureUpsertChange(gwUpdatedSameGen)

				changed, conf, statuses := processor.Process()
				Expect(changed).To(BeFalse())
				Expect(conf).To(BeZero())
				Expect(statuses).To(BeZero())
			})

			It("should return updated configuration and statuses after upserting the first Gateway with generation change", func() {
				processor.CaptureUpsertChange(gw1Updated)

				expectedConf := state.Configuration{
					HTTPServers: []state.VirtualServer{
						{
							Hostname: "foo.example.com",
							PathRules: []state.PathRule{
								{
									Path: "/",
									MatchRules: []state.MatchRule{
										{
											MatchIdx: 0,
											RuleIdx:  0,
											Source:   hr1Updated,
										},
									},
								},
							},
						},
					},
					SSLServers: []state.VirtualServer{
						{
							Hostname: "foo.example.com",
							SSL:      &state.SSL{CertificatePath: certificatePath},
							PathRules: []state.PathRule{
								{
									Path: "/",
									MatchRules: []state.MatchRule{
										{
											MatchIdx: 0,
											RuleIdx:  0,
											Source:   hr1Updated,
										},
									},
								},
							},
						},
						{
							Hostname: "~^",
							SSL:      &state.SSL{CertificatePath: certificatePath},
						},
					},
				}
				expectedStatuses := state.Statuses{
					GatewayClassStatus: &state.GatewayClassStatus{
						Valid:              true,
						ObservedGeneration: gc.Generation,
					},
					GatewayStatus: &state.GatewayStatus{
						NsName: types.NamespacedName{Namespace: "test", Name: "gateway-1"},
						ListenerStatuses: map[string]state.ListenerStatus{
							"listener-80-1": {
								Valid:          true,
								AttachedRoutes: 1,
							},
							"listener-443-1": {
								Valid:          true,
								AttachedRoutes: 1,
							},
						},
					},
					IgnoredGatewayStatuses: map[types.NamespacedName]state.IgnoredGatewayStatus{},
					HTTPRouteStatuses: map[types.NamespacedName]state.HTTPRouteStatus{
						{Namespace: "test", Name: "hr-1"}: {
							ParentStatuses: map[string]state.ParentStatus{
								"listener-80-1":  {Attached: true},
								"listener-443-1": {Attached: true},
							},
						},
					},
				}

				changed, conf, statuses := processor.Process()
				Expect(changed).To(BeTrue())
				Expect(helpers.Diff(expectedConf, conf)).To(BeEmpty())
				Expect(helpers.Diff(expectedStatuses, statuses)).To(BeEmpty())
			})

			It("should return empty configuration and statuses after processing upserting the GatewayClass without generation change", func() {
				gcUpdatedSameGen := gc.DeepCopy()
				// gcUpdatedSameGen.Generation has not been changed
				processor.CaptureUpsertChange(gcUpdatedSameGen)

				changed, conf, statuses := processor.Process()
				Expect(changed).To(BeFalse())
				Expect(conf).To(BeZero())
				Expect(statuses).To(BeZero())
			})

			It("should return updated configuration and statuses after upserting the GatewayClass with generation change", func() {
				processor.CaptureUpsertChange(gcUpdated)

				expectedConf := state.Configuration{
					HTTPServers: []state.VirtualServer{
						{
							Hostname: "foo.example.com",
							PathRules: []state.PathRule{
								{
									Path: "/",
									MatchRules: []state.MatchRule{
										{
											MatchIdx: 0,
											RuleIdx:  0,
											Source:   hr1Updated,
										},
									},
								},
							},
						},
					},
					SSLServers: []state.VirtualServer{
						{
							Hostname: "foo.example.com",
							SSL:      &state.SSL{CertificatePath: certificatePath},
							PathRules: []state.PathRule{
								{
									Path: "/",
									MatchRules: []state.MatchRule{
										{
											MatchIdx: 0,
											RuleIdx:  0,
											Source:   hr1Updated,
										},
									},
								},
							},
						},
						{
							Hostname: "~^",
							SSL:      &state.SSL{CertificatePath: certificatePath},
						},
					},
				}
				expectedStatuses := state.Statuses{
					GatewayClassStatus: &state.GatewayClassStatus{
						Valid:              true,
						ObservedGeneration: gcUpdated.Generation,
					},
					GatewayStatus: &state.GatewayStatus{
						NsName: types.NamespacedName{Namespace: "test", Name: "gateway-1"},
						ListenerStatuses: map[string]state.ListenerStatus{
							"listener-80-1": {
								Valid:          true,
								AttachedRoutes: 1,
							},
							"listener-443-1": {
								Valid:          true,
								AttachedRoutes: 1,
							},
						},
					},
					IgnoredGatewayStatuses: map[types.NamespacedName]state.IgnoredGatewayStatus{},
					HTTPRouteStatuses: map[types.NamespacedName]state.HTTPRouteStatus{
						{Namespace: "test", Name: "hr-1"}: {
							ParentStatuses: map[string]state.ParentStatus{
								"listener-80-1":  {Attached: true},
								"listener-443-1": {Attached: true},
							},
						},
					},
				}

				changed, conf, statuses := processor.Process()
				Expect(changed).To(BeTrue())
				Expect(helpers.Diff(expectedConf, conf)).To(BeEmpty())
				Expect(helpers.Diff(expectedStatuses, statuses)).To(BeEmpty())
			})

			It("should return empty configuration and statuses after processing without capturing any changes", func() {
				changed, conf, statuses := processor.Process()

				Expect(changed).To(BeFalse())
				Expect(conf).To(BeZero())
				Expect(statuses).To(BeZero())
			})

			It("should return updated configuration and statuses after the second Gateway is upserted", func() {
				processor.CaptureUpsertChange(gw2)

				expectedConf := state.Configuration{
					HTTPServers: []state.VirtualServer{
						{
							Hostname: "foo.example.com",
							PathRules: []state.PathRule{
								{
									Path: "/",
									MatchRules: []state.MatchRule{
										{
											MatchIdx: 0,
											RuleIdx:  0,
											Source:   hr1Updated,
										},
									},
								},
							},
						},
					},
					SSLServers: []state.VirtualServer{
						{
							Hostname: "foo.example.com",
							PathRules: []state.PathRule{
								{
									Path: "/",
									MatchRules: []state.MatchRule{
										{
											MatchIdx: 0,
											RuleIdx:  0,
											Source:   hr1Updated,
										},
									},
								},
							},
							SSL: &state.SSL{
								CertificatePath: certificatePath,
							},
						},
						{
							Hostname: "~^",
							SSL:      &state.SSL{CertificatePath: certificatePath},
						},
					},
				}
				expectedStatuses := state.Statuses{
					GatewayClassStatus: &state.GatewayClassStatus{
						Valid:              true,
						ObservedGeneration: gcUpdated.Generation,
					},
					GatewayStatus: &state.GatewayStatus{
						NsName: types.NamespacedName{Namespace: "test", Name: "gateway-1"},
						ListenerStatuses: map[string]state.ListenerStatus{
							"listener-80-1": {
								Valid:          true,
								AttachedRoutes: 1,
							},
							"listener-443-1": {
								Valid:          true,
								AttachedRoutes: 1,
							},
						},
					},
					IgnoredGatewayStatuses: map[types.NamespacedName]state.IgnoredGatewayStatus{
						{Namespace: "test", Name: "gateway-2"}: {
							ObservedGeneration: gw2.Generation,
						},
					},
					HTTPRouteStatuses: map[types.NamespacedName]state.HTTPRouteStatus{
						{Namespace: "test", Name: "hr-1"}: {
							ParentStatuses: map[string]state.ParentStatus{
								"listener-80-1":  {Attached: true},
								"listener-443-1": {Attached: true},
							},
						},
					},
				}

				changed, conf, statuses := processor.Process()
				Expect(changed).To(BeTrue())
				Expect(helpers.Diff(expectedConf, conf)).To(BeEmpty())
				Expect(helpers.Diff(expectedStatuses, statuses)).To(BeEmpty())
			})

			It("should return same configuration and updated statuses after the second HTTPRoute is upserted", func() {
				processor.CaptureUpsertChange(hr2)

				expectedConf := state.Configuration{
					HTTPServers: []state.VirtualServer{
						{
							Hostname: "foo.example.com",
							PathRules: []state.PathRule{
								{
									Path: "/",
									MatchRules: []state.MatchRule{
										{
											MatchIdx: 0,
											RuleIdx:  0,
											Source:   hr1Updated,
										},
									},
								},
							},
						},
					},
					SSLServers: []state.VirtualServer{
						{
							Hostname: "foo.example.com",
							SSL:      &state.SSL{CertificatePath: certificatePath},
							PathRules: []state.PathRule{
								{
									Path: "/",
									MatchRules: []state.MatchRule{
										{
											MatchIdx: 0,
											RuleIdx:  0,
											Source:   hr1Updated,
										},
									},
								},
							},
						},
						{
							Hostname: "~^",
							SSL:      &state.SSL{CertificatePath: certificatePath},
						},
					},
				}
				expectedStatuses := state.Statuses{
					GatewayClassStatus: &state.GatewayClassStatus{
						Valid:              true,
						ObservedGeneration: gcUpdated.Generation,
					},
					GatewayStatus: &state.GatewayStatus{
						NsName: types.NamespacedName{Namespace: "test", Name: "gateway-1"},
						ListenerStatuses: map[string]state.ListenerStatus{
							"listener-80-1": {
								Valid:          true,
								AttachedRoutes: 1,
							},
							"listener-443-1": {
								Valid:          true,
								AttachedRoutes: 1,
							},
						},
					},
					IgnoredGatewayStatuses: map[types.NamespacedName]state.IgnoredGatewayStatus{
						{Namespace: "test", Name: "gateway-2"}: {
							ObservedGeneration: gw2.Generation,
						},
					},
					HTTPRouteStatuses: map[types.NamespacedName]state.HTTPRouteStatus{
						{Namespace: "test", Name: "hr-1"}: {
							ParentStatuses: map[string]state.ParentStatus{
								"listener-80-1":  {Attached: true},
								"listener-443-1": {Attached: true},
							},
						},
						{Namespace: "test", Name: "hr-2"}: {
							ParentStatuses: map[string]state.ParentStatus{
								"listener-80-1":  {Attached: false},
								"listener-443-1": {Attached: false},
							},
						},
					},
				}

				changed, conf, statuses := processor.Process()
				Expect(changed).To(BeTrue())
				Expect(helpers.Diff(expectedConf, conf)).To(BeEmpty())
				Expect(helpers.Diff(expectedStatuses, statuses)).To(BeEmpty())
			})

			It("should return updated configuration and statuses after deleting the first Gateway", func() {
				processor.CaptureDeleteChange(&v1beta1.Gateway{}, types.NamespacedName{Namespace: "test", Name: "gateway-1"})

				expectedConf := state.Configuration{
					HTTPServers: []state.VirtualServer{
						{
							Hostname: "bar.example.com",
							PathRules: []state.PathRule{
								{
									Path: "/",
									MatchRules: []state.MatchRule{
										{
											MatchIdx: 0,
											RuleIdx:  0,
											Source:   hr2,
										},
									},
								},
							},
						},
					},
					SSLServers: []state.VirtualServer{
						{
							Hostname: "bar.example.com",
							SSL:      &state.SSL{CertificatePath: certificatePath},
							PathRules: []state.PathRule{
								{
									Path: "/",
									MatchRules: []state.MatchRule{
										{
											MatchIdx: 0,
											RuleIdx:  0,
											Source:   hr2,
										},
									},
								},
							},
						},
						{
							Hostname: "~^",
							SSL:      &state.SSL{CertificatePath: certificatePath},
						},
					},
				}
				expectedStatuses := state.Statuses{
					GatewayClassStatus: &state.GatewayClassStatus{
						Valid:              true,
						ObservedGeneration: gcUpdated.Generation,
					},
					GatewayStatus: &state.GatewayStatus{
						NsName: types.NamespacedName{Namespace: "test", Name: "gateway-2"},
						ListenerStatuses: map[string]state.ListenerStatus{
							"listener-80-1": {
								Valid:          true,
								AttachedRoutes: 1,
							},
							"listener-443-1": {
								Valid:          true,
								AttachedRoutes: 1,
							},
						},
					},
					IgnoredGatewayStatuses: map[types.NamespacedName]state.IgnoredGatewayStatus{},
					HTTPRouteStatuses: map[types.NamespacedName]state.HTTPRouteStatus{
						{Namespace: "test", Name: "hr-2"}: {
							ParentStatuses: map[string]state.ParentStatus{
								"listener-80-1":  {Attached: true},
								"listener-443-1": {Attached: true},
							},
						},
					},
				}

				changed, conf, statuses := processor.Process()
				Expect(changed).To(BeTrue())
				Expect(helpers.Diff(expectedConf, conf)).To(BeEmpty())
				Expect(helpers.Diff(expectedStatuses, statuses)).To(BeEmpty())
			})

			It("should return configuration with default ssl server and updated statuses after deleting the second HTTPRoute", func() {
				processor.CaptureDeleteChange(&v1beta1.HTTPRoute{}, types.NamespacedName{Namespace: "test", Name: "hr-2"})

				expectedConf := state.Configuration{
					HTTPServers: []state.VirtualServer{},
					SSLServers: []state.VirtualServer{
						{
							Hostname: "~^",
							SSL:      &state.SSL{CertificatePath: certificatePath},
						},
					},
				}
				expectedStatuses := state.Statuses{
					GatewayClassStatus: &state.GatewayClassStatus{
						Valid:              true,
						ObservedGeneration: gcUpdated.Generation,
					},
					GatewayStatus: &state.GatewayStatus{
						NsName: types.NamespacedName{Namespace: "test", Name: "gateway-2"},
						ListenerStatuses: map[string]state.ListenerStatus{
							"listener-80-1": {
								Valid:          true,
								AttachedRoutes: 0,
							},
							"listener-443-1": {
								Valid:          true,
								AttachedRoutes: 0,
							},
						},
					},
					IgnoredGatewayStatuses: map[types.NamespacedName]state.IgnoredGatewayStatus{},
					HTTPRouteStatuses:      map[types.NamespacedName]state.HTTPRouteStatus{},
				}

				changed, conf, statuses := processor.Process()
				Expect(changed).To(BeTrue())
				Expect(helpers.Diff(expectedConf, conf)).To(BeEmpty())
				Expect(helpers.Diff(expectedStatuses, statuses)).To(BeEmpty())
			})

			It("should return empty configuration and updated statuses after deleting the GatewayClass", func() {
				processor.CaptureDeleteChange(&v1beta1.GatewayClass{}, types.NamespacedName{Name: gcName})

				expectedConf := state.Configuration{}
				expectedStatuses := state.Statuses{
					GatewayStatus: &state.GatewayStatus{
						NsName: types.NamespacedName{Namespace: "test", Name: "gateway-2"},
						ListenerStatuses: map[string]state.ListenerStatus{
							"listener-80-1": {
								Valid:          false,
								AttachedRoutes: 0,
							},
							"listener-443-1": {
								Valid:          false,
								AttachedRoutes: 0,
							},
						},
					},
					IgnoredGatewayStatuses: map[types.NamespacedName]state.IgnoredGatewayStatus{},
					HTTPRouteStatuses:      map[types.NamespacedName]state.HTTPRouteStatus{},
				}

				changed, conf, statuses := processor.Process()
				Expect(changed).To(BeTrue())
				Expect(helpers.Diff(expectedConf, conf)).To(BeEmpty())
				Expect(helpers.Diff(expectedStatuses, statuses)).To(BeEmpty())
			})

			It("should return empty configuration and empty statuses after deleting the second Gateway", func() {
				processor.CaptureDeleteChange(&v1beta1.Gateway{}, types.NamespacedName{Namespace: "test", Name: "gateway-2"})

				expectedConf := state.Configuration{}
				expectedStatuses := state.Statuses{
					IgnoredGatewayStatuses: map[types.NamespacedName]state.IgnoredGatewayStatus{},
					HTTPRouteStatuses:      map[types.NamespacedName]state.HTTPRouteStatus{},
				}

				changed, conf, statuses := processor.Process()
				Expect(changed).To(BeTrue())
				Expect(helpers.Diff(expectedConf, conf)).To(BeEmpty())
				Expect(helpers.Diff(expectedStatuses, statuses)).To(BeEmpty())
			})

			It("should return empty configuration and statuses after deleting the first HTTPRoute", func() {
				processor.CaptureDeleteChange(&v1beta1.HTTPRoute{}, types.NamespacedName{Namespace: "test", Name: "hr-1"})

				expectedConf := state.Configuration{}
				expectedStatuses := state.Statuses{
					IgnoredGatewayStatuses: map[types.NamespacedName]state.IgnoredGatewayStatus{},
					HTTPRouteStatuses:      map[types.NamespacedName]state.HTTPRouteStatus{},
				}

				changed, conf, statuses := processor.Process()
				Expect(changed).To(BeTrue())
				Expect(helpers.Diff(expectedConf, conf)).To(BeEmpty())
				Expect(helpers.Diff(expectedStatuses, statuses)).To(BeEmpty())
			})
		})
	})

	Describe("Multiple captured changes", func() {
		var (
			processor                    *state.ChangeProcessorImpl
			gcNsName, gwNsName, hrNsName types.NamespacedName
			gc, gcUpdated                *v1beta1.GatewayClass
			gw1, gw1Updated, gw2         *v1beta1.Gateway
			hr1, hr1Updated, hr2         *v1beta1.HTTPRoute
		)

		BeforeEach(OncePerOrdered, func() {
			fakeSecretMemoryMgr := &statefakes.FakeSecretDiskMemoryManager{}
			processor = state.NewChangeProcessorImpl(state.ChangeProcessorConfig{
				GatewayCtlrName:     "test.controller",
				GatewayClassName:    "my-class",
				SecretMemoryManager: fakeSecretMemoryMgr,
			})

			gcNsName = types.NamespacedName{Name: "my-class"}

			gc = &v1beta1.GatewayClass{
				ObjectMeta: metav1.ObjectMeta{
					Name: gcNsName.Name,
				},
				Spec: v1beta1.GatewayClassSpec{
					ControllerName: "test.controller",
				},
			}

			gcUpdated = gc.DeepCopy()
			gcUpdated.Generation++

			gwNsName = types.NamespacedName{Namespace: "test", Name: "gw-1"}

			gw1 = &v1beta1.Gateway{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: gwNsName.Namespace,
					Name:      gwNsName.Name,
				},
			}

			gw1Updated = gw1.DeepCopy()
			gw1Updated.Generation++

			gw2 = gw1.DeepCopy()
			gw2.Name = "gw-2"

			hrNsName = types.NamespacedName{Namespace: "test", Name: "hr-1"}

			hr1 = &v1beta1.HTTPRoute{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: hrNsName.Namespace,
					Name:      hrNsName.Name,
				},
			}

			hr1Updated = hr1.DeepCopy()
			hr1Updated.Generation++

			hr2 = hr1.DeepCopy()
			hr2.Name = "hr-2"
		})

		Describe("Ensuring non-changing changes don't override previously changing changes", Ordered, func() {
			// Changing change - a change that makes processor.Process() report changed
			// Non-changing change - a change that doesn't do that

			// Note: in this test, we deliberately don't fully inspect the returned configuration and statuses
			// -- this is done in 'Normal cases of processing changes'

			It("should report changed after multiple Upserts", func() {
				processor.CaptureUpsertChange(gc)
				processor.CaptureUpsertChange(gw1)
				processor.CaptureUpsertChange(hr1)

				changed, _, _ := processor.Process()
				Expect(changed).To(BeTrue())
			})

			It("should report not changed after multiple Upserts of the resource with same generation", func() {
				processor.CaptureUpsertChange(gc)
				processor.CaptureUpsertChange(gw1)
				processor.CaptureUpsertChange(hr1)

				changed, _, _ := processor.Process()
				Expect(changed).To(BeFalse())
			})

			It("should report changed after upserting updated resources followed by upserting same generations", func() {
				// these are changing changes
				processor.CaptureUpsertChange(gcUpdated)
				processor.CaptureUpsertChange(gw1Updated)
				processor.CaptureUpsertChange(hr1Updated)

				// there are non-changing changes
				processor.CaptureUpsertChange(gcUpdated)
				processor.CaptureUpsertChange(gw1Updated)
				processor.CaptureUpsertChange(hr1Updated)

				changed, _, _ := processor.Process()
				Expect(changed).To(BeTrue())
			})

			It("should report changed after upserting new resources", func() {
				// we can't have a second GatewayClass, so we don't add it
				processor.CaptureUpsertChange(gw2)
				processor.CaptureUpsertChange(hr2)

				changed, _, _ := processor.Process()
				Expect(changed).To(BeTrue())
			})

			It("should report changed after deleting resources followed by upserting same generations of new resources", func() {
				// these are changing changes
				processor.CaptureDeleteChange(&v1beta1.GatewayClass{}, gcNsName)
				processor.CaptureDeleteChange(&v1beta1.Gateway{}, gwNsName)
				processor.CaptureDeleteChange(&v1beta1.HTTPRoute{}, hrNsName)

				// these are non-changing changes
				processor.CaptureUpsertChange(gw2)
				processor.CaptureUpsertChange(hr2)

				changed, _, _ := processor.Process()
				Expect(changed).To(BeTrue())
			})
		})
	})

	Describe("Edge cases with panic", func() {
		var processor state.ChangeProcessor
		var fakeSecretMemoryMgr *statefakes.FakeSecretDiskMemoryManager

		BeforeEach(func() {
			fakeSecretMemoryMgr = &statefakes.FakeSecretDiskMemoryManager{}

			processor = state.NewChangeProcessorImpl(state.ChangeProcessorConfig{
				GatewayCtlrName:     "test.controller",
				GatewayClassName:    "my-class",
				SecretMemoryManager: fakeSecretMemoryMgr,
			})
		})

		DescribeTable("CaptureUpsertChange must panic",
			func(obj client.Object) {
				process := func() {
					processor.CaptureUpsertChange(obj)
				}
				Expect(process).Should(Panic())
			},
			Entry("an unsupported resource", &v1alpha2.TCPRoute{ObjectMeta: metav1.ObjectMeta{Namespace: "test", Name: "tcp"}}),
			Entry("a wrong gatewayclass", &v1beta1.GatewayClass{ObjectMeta: metav1.ObjectMeta{Name: "wrong-class"}}))

		DescribeTable("CaptureDeleteChange must panic",
			func(resourceType client.Object, nsname types.NamespacedName) {
				process := func() {
					processor.CaptureDeleteChange(resourceType, nsname)
				}
				Expect(process).Should(Panic())
			},
			Entry("an unsupported resource", &v1alpha2.TCPRoute{}, types.NamespacedName{Namespace: "test", Name: "tcp"}),
			Entry("a wrong gatewayclass", &v1beta1.GatewayClass{}, types.NamespacedName{Name: "wrong-class"}))
	})
})
