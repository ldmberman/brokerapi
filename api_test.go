package brokerapi_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"

	"github.com/drewolson/testflight"
	"github.com/pivotal-golang/lager"
	"github.com/pivotal-golang/lager/lagertest"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"

	"github.com/ldmberman/brokerapi"
	"github.com/ldmberman/brokerapi/fakes"
)

var _ = Describe("Service Broker API", func() {
	var fakeServiceBroker *fakes.FakeServiceBroker
	var brokerAPI http.Handler
	var brokerLogger *lagertest.TestLogger
	var credentials = brokerapi.BrokerCredentials{
		Username: "username",
		Password: "password",
	}

	makeInstanceProvisioningRequest := func(instanceID string, details brokerapi.ProvisionDetails, queryString string) *testflight.Response {
		response := &testflight.Response{}

		testflight.WithServer(brokerAPI, func(r *testflight.Requester) {
			path := "/v2/service_instances/" + instanceID + queryString

			buffer := &bytes.Buffer{}
			json.NewEncoder(buffer).Encode(details)
			request, err := http.NewRequest("PUT", path, buffer)
			Expect(err).NotTo(HaveOccurred())
			request.Header.Add("Content-Type", "application/json")
			request.SetBasicAuth(credentials.Username, credentials.Password)

			response = r.Do(request)
		})
		return response
	}

	makeInstanceProvisioningRequestWithAcceptsIncomplete := func(instanceID string, details brokerapi.ProvisionDetails, acceptsIncomplete bool) *testflight.Response {
		var acceptsIncompleteFlag string

		if acceptsIncomplete {
			acceptsIncompleteFlag = "?accepts_incomplete=true"
		} else {
			acceptsIncompleteFlag = "?accepts_incomplete=false"
		}

		return makeInstanceProvisioningRequest(instanceID, details, acceptsIncompleteFlag)
	}

	makeInstanceUpdateRequest := func(instanceID string, details interface{}, queryString string) *testflight.Response {
		response := &testflight.Response{}

		testflight.WithServer(brokerAPI, func(r *testflight.Requester) {
			path := "/v2/service_instances/" + instanceID + queryString

			buffer := &bytes.Buffer{}
			json.NewEncoder(buffer).Encode(details)
			request, err := http.NewRequest("PATCH", path, buffer)
			Expect(err).NotTo(HaveOccurred())
			request.Header.Add("Content-Type", "application/json")
			request.SetBasicAuth(credentials.Username, credentials.Password)

			response = r.Do(request)
		})
		return response
	}

	makeInstanceUpdateRequestWithAcceptsIncomplete := func(instanceID string, details brokerapi.UpdateDetails, acceptsIncomplete bool) *testflight.Response {
		var acceptsIncompleteFlag string

		if acceptsIncomplete {
			acceptsIncompleteFlag = "?accepts_incomplete=true"
		} else {
			acceptsIncompleteFlag = "?accepts_incomplete=false"
		}

		return makeInstanceUpdateRequest(instanceID, details, acceptsIncompleteFlag)
	}

	lastLogLine := func() lager.LogFormat {
		noOfLogLines := len(brokerLogger.Logs())
		if noOfLogLines == 0 {
			// better way to raise error?
			err := errors.New("expected some log lines but there were none!")
			Expect(err).NotTo(HaveOccurred())
		}

		return brokerLogger.Logs()[noOfLogLines-1]
	}

	BeforeEach(func() {
		fakeServiceBroker = &fakes.FakeServiceBroker{
			InstanceLimit: 3,
		}
		brokerLogger = lagertest.NewTestLogger("broker-api")
		brokerAPI = brokerapi.New(fakeServiceBroker, brokerLogger, credentials)
	})

	Describe("respose headers", func() {
		makeRequest := func() *httptest.ResponseRecorder {
			recorder := httptest.NewRecorder()
			request, _ := http.NewRequest("GET", "/v2/catalog", nil)
			request.SetBasicAuth(credentials.Username, credentials.Password)
			brokerAPI.ServeHTTP(recorder, request)
			return recorder
		}

		It("has a Content-Type header", func() {
			response := makeRequest()

			header := response.Header().Get("Content-Type")
			Ω(header).Should(Equal("application/json"))
		})
	})

	Describe("authentication", func() {
		makeRequestWithoutAuth := func() *testflight.Response {
			response := &testflight.Response{}
			testflight.WithServer(brokerAPI, func(r *testflight.Requester) {
				request, _ := http.NewRequest("GET", "/v2/catalog", nil)
				response = r.Do(request)
			})
			return response
		}

		makeRequestWithAuth := func(username string, password string) *testflight.Response {
			response := &testflight.Response{}
			testflight.WithServer(brokerAPI, func(r *testflight.Requester) {
				request, _ := http.NewRequest("GET", "/v2/catalog", nil)
				request.SetBasicAuth(username, password)

				response = r.Do(request)
			})
			return response
		}

		makeRequestWithUnrecognizedAuth := func() *testflight.Response {
			response := &testflight.Response{}
			testflight.WithServer(brokerAPI, func(r *testflight.Requester) {
				request, _ := http.NewRequest("GET", "/v2/catalog", nil)
				// dXNlcm5hbWU6cGFzc3dvcmQ= is base64 encoding of 'username:password',
				// ie, a correctly encoded basic authorization header
				request.Header["Authorization"] = []string{"NOTBASIC dXNlcm5hbWU6cGFzc3dvcmQ="}

				response = r.Do(request)
			})
			return response
		}

		It("returns 401 when the authorization header has an incorrect password", func() {
			response := makeRequestWithAuth("username", "fake_password")
			Expect(response.StatusCode).To(Equal(401))
		})

		It("returns 401 when the authorization header has an incorrect username", func() {
			response := makeRequestWithAuth("fake_username", "password")
			Expect(response.StatusCode).To(Equal(401))
		})

		It("returns 401 when there is no authorization header", func() {
			response := makeRequestWithoutAuth()
			Expect(response.StatusCode).To(Equal(401))
		})

		It("returns 401 when there is a unrecognized authorization header", func() {
			response := makeRequestWithUnrecognizedAuth()
			Expect(response.StatusCode).To(Equal(401))
		})

		It("does not call through to the service broker when not authenticated", func() {
			makeRequestWithAuth("username", "fake_password")
			Ω(fakeServiceBroker.BrokerCalled).ShouldNot(BeTrue(),
				"broker should not have been hit when authentication failed",
			)
		})
	})

	Describe("catalog endpoint", func() {
		makeCatalogRequest := func() *testflight.Response {
			response := &testflight.Response{}
			testflight.WithServer(brokerAPI, func(r *testflight.Requester) {
				request, _ := http.NewRequest("GET", "/v2/catalog", nil)
				request.SetBasicAuth("username", "password")

				response = r.Do(request)
			})
			return response
		}

		It("returns a 200", func() {
			response := makeCatalogRequest()
			Expect(response.StatusCode).To(Equal(200))
		})

		It("returns valid catalog json", func() {
			response := makeCatalogRequest()
			Expect(response.Body).To(MatchJSON(fixture("catalog.json")))
		})
	})

	Describe("instance lifecycle endpoint", func() {
		makeInstanceDeprovisioningRequest := func(instanceID string) *testflight.Response {
			response := &testflight.Response{}
			testflight.WithServer(brokerAPI, func(r *testflight.Requester) {
				path := "/v2/service_instances/" + instanceID
				request, _ := http.NewRequest("DELETE", path, strings.NewReader(""))
				request.Header.Add("Content-Type", "application/json")
				request.SetBasicAuth("username", "password")

				response = r.Do(request)

			})
			return response
		}

		Describe("provisioning", func() {
			var instanceID string
			var details brokerapi.ProvisionDetails

			BeforeEach(func() {
				instanceID = uniqueInstanceID()
				details = brokerapi.ProvisionDetails{
					ID:               "service-id",
					PlanID:           "plan-id",
					OrganizationGUID: "organization-guid",
					SpaceGUID:        "space-guid",
				}
			})

			It("calls Provision on the service broker with all params", func() {
				makeInstanceProvisioningRequest(instanceID, details, "")
				Expect(fakeServiceBroker.ProvisionDetails).To(Equal(details))
			})

			It("calls Provision on the service broker with the instance id", func() {
				makeInstanceProvisioningRequest(instanceID, details, "")
				Expect(fakeServiceBroker.ProvisionedInstanceIDs).To(ContainElement(instanceID))
			})

			Context("when there are arbitrary params", func() {
				BeforeEach(func() {
					details.Parameters = map[string]interface{}{
						"string": "some-string",
						"number": 1,
						"object": struct{ Name string }{"some-name"},
						"array":  []interface{}{"a", "b", "c"},
					}
				})

				It("calls Provision on the service broker with all params", func() {
					makeInstanceProvisioningRequest(instanceID, details, "")
					Expect(fakeServiceBroker.ProvisionDetails.Parameters["string"]).To(Equal("some-string"))
					Expect(fakeServiceBroker.ProvisionDetails.Parameters["number"]).To(Equal(1.0))
					Expect(fakeServiceBroker.ProvisionDetails.Parameters["array"]).To(Equal([]interface{}{"a", "b", "c"}))
					actual, _ := fakeServiceBroker.ProvisionDetails.Parameters["object"].(map[string]interface{})
					Expect(actual["Name"]).To(Equal("some-name"))
				})
			})

			Context("when the instance does not exist", func() {
				It("returns a 201", func() {
					response := makeInstanceProvisioningRequest(instanceID, details, "")
					Expect(response.StatusCode).To(Equal(201))
				})

				It("returns json with a dashboard_url field", func() {
					response := makeInstanceProvisioningRequest(instanceID, details, "")
					Expect(response.Body).To(MatchJSON(fixture("provisioning.json")))
				})

				Context("when the instance limit has been reached", func() {
					BeforeEach(func() {
						for i := 0; i < fakeServiceBroker.InstanceLimit; i++ {
							makeInstanceProvisioningRequest(uniqueInstanceID(), details, "")
						}
					})

					It("returns a 500", func() {
						response := makeInstanceProvisioningRequest(instanceID, details, "")
						Expect(response.StatusCode).To(Equal(500))
					})

					It("returns json with a description field and a useful error message", func() {
						response := makeInstanceProvisioningRequest(instanceID, details, "")
						Expect(response.Body).To(MatchJSON(fixture("instance_limit_error.json")))
					})

					It("logs an appropriate error", func() {
						makeInstanceProvisioningRequest(instanceID, details, "")

						Expect(lastLogLine().Message).To(ContainSubstring("provision.instance-limit-reached"))
						Expect(lastLogLine().Data["error"]).To(ContainSubstring("instance limit for this service has been reached"))
					})
				})

				Context("when an unexpected error occurs", func() {
					BeforeEach(func() {
						fakeServiceBroker.ProvisionError = errors.New("broker failed")
					})

					It("returns a 500", func() {
						response := makeInstanceProvisioningRequest(instanceID, details, "")
						Expect(response.StatusCode).To(Equal(500))
					})

					It("returns json with a description field and a useful error message", func() {
						response := makeInstanceProvisioningRequest(instanceID, details, "")
						Expect(response.Body).To(MatchJSON(`{"description":"broker failed"}`))
					})

					It("logs an appropriate error", func() {
						makeInstanceProvisioningRequest(instanceID, details, "")
						Expect(lastLogLine().Message).To(ContainSubstring("provision.unknown-error"))
						Expect(lastLogLine().Data["error"]).To(ContainSubstring("broker failed"))
					})
				})

				Context("when we send invalid json", func() {
					makeBadInstanceProvisioningRequest := func(instanceID string) *testflight.Response {
						response := &testflight.Response{}

						testflight.WithServer(brokerAPI, func(r *testflight.Requester) {
							path := "/v2/service_instances/" + instanceID

							body := strings.NewReader("{{{{{")
							request, err := http.NewRequest("PUT", path, body)
							Expect(err).NotTo(HaveOccurred())
							request.Header.Add("Content-Type", "application/json")
							request.SetBasicAuth(credentials.Username, credentials.Password)

							response = r.Do(request)
						})

						return response
					}

					It("returns a 422 bad request", func() {
						response := makeBadInstanceProvisioningRequest(instanceID)
						Expect(response.StatusCode).Should(Equal(422))
					})

					It("logs a message", func() {
						makeBadInstanceProvisioningRequest(instanceID)
						Expect(lastLogLine().Message).To(ContainSubstring("provision.invalid-service-details"))
					})
				})
			})

			Context("when the instance already exists", func() {
				BeforeEach(func() {
					makeInstanceProvisioningRequest(instanceID, details, "")
				})

				It("returns a 409", func() {
					response := makeInstanceProvisioningRequest(instanceID, details, "")
					Expect(response.StatusCode).To(Equal(409))
				})

				It("returns an empty JSON object", func() {
					response := makeInstanceProvisioningRequest(instanceID, details, "")
					Expect(response.Body).To(MatchJSON(`{}`))
				})

				It("logs an appropriate error", func() {
					makeInstanceProvisioningRequest(instanceID, details, "")
					Expect(lastLogLine().Message).To(ContainSubstring("provision.instance-already-exists"))
					Expect(lastLogLine().Data["error"]).To(ContainSubstring("instance already exists"))
				})
			})

			Describe("accepts_incomplete", func() {
				Context("when the accepts_incomplete flag is true", func() {
					It("calls ProvisionAsync on the service broker", func() {
						acceptsIncomplete := true
						makeInstanceProvisioningRequestWithAcceptsIncomplete(instanceID, details, acceptsIncomplete)
						Expect(fakeServiceBroker.ProvisionDetails).To(Equal(details))

						Expect(fakeServiceBroker.ProvisionedInstanceIDs).To(ContainElement(instanceID))
					})

					Context("when the broker chooses to provision asyncronously", func() {
						BeforeEach(func() {
							fakeServiceBroker = &fakes.FakeServiceBroker{
								InstanceLimit: 3,
							}
							fakeAsyncServiceBroker := &fakes.FakeAsyncServiceBroker{
								FakeServiceBroker:    *fakeServiceBroker,
								ShouldProvisionAsync: true,
							}
							brokerAPI = brokerapi.New(fakeAsyncServiceBroker, brokerLogger, credentials)
						})

						It("returns a 202", func() {
							response := makeInstanceProvisioningRequestWithAcceptsIncomplete(instanceID, details, true)
							Expect(response.StatusCode).To(Equal(http.StatusAccepted))
						})
					})

					Context("when the broker chooses to provision syncronously", func() {
						BeforeEach(func() {
							fakeServiceBroker = &fakes.FakeServiceBroker{
								InstanceLimit: 3,
							}
							fakeAsyncServiceBroker := &fakes.FakeAsyncServiceBroker{
								FakeServiceBroker:    *fakeServiceBroker,
								ShouldProvisionAsync: false,
							}
							brokerAPI = brokerapi.New(fakeAsyncServiceBroker, brokerLogger, credentials)
						})

						It("returns a 201", func() {
							response := makeInstanceProvisioningRequestWithAcceptsIncomplete(instanceID, details, true)
							Expect(response.StatusCode).To(Equal(http.StatusCreated))
						})
					})
				})

				Context("when the accepts_incomplete flag is false", func() {
					It("returns a 201", func() {
						response := makeInstanceProvisioningRequestWithAcceptsIncomplete(instanceID, details, false)
						Expect(response.StatusCode).To(Equal(http.StatusCreated))
					})

					Context("when broker can only respond asynchronously", func() {
						BeforeEach(func() {
							fakeServiceBroker = &fakes.FakeServiceBroker{
								InstanceLimit: 3,
							}
							fakeAsyncServiceBroker := &fakes.FakeAsyncOnlyServiceBroker{
								*fakeServiceBroker,
							}
							brokerAPI = brokerapi.New(fakeAsyncServiceBroker, brokerLogger, credentials)
						})

						It("returns a 422", func() {
							acceptsIncomplete := false
							response := makeInstanceProvisioningRequestWithAcceptsIncomplete(instanceID, details, acceptsIncomplete)
							Expect(response.StatusCode).To(Equal(422))
							Expect(response.Body).To(MatchJSON(fixture("async_required.json")))
						})
					})
				})

				Context("when the accepts_incomplete flag is missing", func() {
					It("returns a 201", func() {
						response := makeInstanceProvisioningRequest(instanceID, details, "")
						Expect(response.StatusCode).To(Equal(http.StatusCreated))
					})

					Context("when broker can only respond asynchronously", func() {
						BeforeEach(func() {
							fakeServiceBroker = &fakes.FakeServiceBroker{
								InstanceLimit: 3,
							}
							fakeAsyncServiceBroker := &fakes.FakeAsyncOnlyServiceBroker{
								*fakeServiceBroker,
							}
							brokerAPI = brokerapi.New(fakeAsyncServiceBroker, brokerLogger, credentials)
						})

						It("returns a 422", func() {
							acceptsIncomplete := false
							response := makeInstanceProvisioningRequestWithAcceptsIncomplete(instanceID, details, acceptsIncomplete)
							Expect(response.StatusCode).To(Equal(422))
							Expect(response.Body).To(MatchJSON(fixture("async_required.json")))
						})
					})
				})
			})
		})

		Describe("updating instances", func() {
			var (
				details                       brokerapi.UpdateDetails
				serviceID, planID, instanceID string
			)
			BeforeEach(func() {
				serviceID = "service-id"
				planID = "plan-id"
				instanceID = uniqueInstanceID()
				details = brokerapi.UpdateDetails{
					ID:     serviceID,
					PlanID: planID,
					Parameters: map[string]interface{}{
						"memory": float64(1000),
					},
				}
			})
			Context("when the instance does not exist", func() {
				It("returns a 404", func() {
					response := makeInstanceUpdateRequest(instanceID, details, "")
					Expect(response.StatusCode).To(Equal(404))
				})
				It("returns an empty JSON object", func() {
					response := makeInstanceUpdateRequest(instanceID, details, "")
					Expect(response.Body).To(MatchJSON(`{}`))
				})
				It("logs an appropriate error", func() {
					makeInstanceUpdateRequest(instanceID, details, "")
					Expect(lastLogLine().Message).To(ContainSubstring("update.instance-missing"))
					Expect(lastLogLine().Data["error"]).To(ContainSubstring("instance does not exist"))
				})
			})
			Context("when the instance exists", func() {
				BeforeEach(func() {
					makeInstanceProvisioningRequest(instanceID, brokerapi.ProvisionDetails{
						ID:               serviceID,
						PlanID:           planID,
						OrganizationGUID: "organization-guid",
						SpaceGUID:        "space-guid",
					}, "")
				})
				It("calls Update on the service broker properly passing the params", func() {
					makeInstanceUpdateRequest(instanceID, details, "")
					Expect(fakeServiceBroker.UpdateDetails.ID).To(Equal(details.ID))
					Expect(fakeServiceBroker.UpdateDetails.PlanID).To(Equal(details.PlanID))
					Expect(fakeServiceBroker.UpdateDetails.Parameters["memory"]).To(Equal(details.Parameters["memory"]))
				})
			})
			Context("when an unexpected error occurs", func() {
				BeforeEach(func() {
					fakeServiceBroker.UpdateError = errors.New("update failed")
				})

				It("returns a 500", func() {
					response := makeInstanceUpdateRequest(instanceID, details, "")
					Expect(response.StatusCode).To(Equal(500))
				})

				It("returns json with a description field and a useful error message", func() {
					response := makeInstanceUpdateRequest(instanceID, details, "")
					Expect(response.Body).To(MatchJSON(`{"description":"update failed"}`))
				})

				It("logs an appropriate error", func() {
					makeInstanceUpdateRequest(instanceID, details, "")
					Expect(lastLogLine().Message).To(ContainSubstring("update.unknown-error"))
					Expect(lastLogLine().Data["error"]).To(ContainSubstring("update failed"))
				})
			})
			Context("when invalid JSON is sent", func() {
				It("returns a 422", func() {
					response := makeInstanceUpdateRequest(instanceID, "{{{", "")
					Expect(response.StatusCode).To(Equal(422))
				})
				It("logs a message", func() {
					makeInstanceUpdateRequest(instanceID, "{{{", "")
					Expect(lastLogLine().Message).To(ContainSubstring("update.invalid-update-details"))
				})
			})
		})

		Describe("updating instances asynchronously", func() {
			var (
				instanceID string
			)
			BeforeEach(func() {
				instanceID = uniqueInstanceID()
			})
			Context("when the broker chooses to update asynchronously", func() {
				BeforeEach(func() {
					fakeAsyncServiceBroker := &fakes.FakeAsyncServiceBroker{
						ShouldProvisionAsync: true,
					}
					brokerAPI = brokerapi.New(fakeAsyncServiceBroker, brokerLogger, credentials)
				})
				It("returns a 202", func() {
					response := makeInstanceUpdateRequestWithAcceptsIncomplete(instanceID, brokerapi.UpdateDetails{}, true)
					Expect(response.StatusCode).To(Equal(http.StatusAccepted))
				})
			})
			Context("when the broker chooses to update synchronously", func() {
				BeforeEach(func() {
					fakeAsyncServiceBroker := &fakes.FakeAsyncServiceBroker{
						ShouldProvisionAsync: false,
					}
					brokerAPI = brokerapi.New(fakeAsyncServiceBroker, brokerLogger, credentials)
				})

				It("returns a 200", func() {
					response := makeInstanceUpdateRequestWithAcceptsIncomplete(instanceID, brokerapi.UpdateDetails{}, true)
					Expect(response.StatusCode).To(Equal(http.StatusOK))
				})
			})
			Context("when the accepts_incomplete flag is false", func() {
				BeforeEach(func() {
					fakeAsyncServiceBroker := &fakes.FakeAsyncServiceBroker{}
					brokerAPI = brokerapi.New(fakeAsyncServiceBroker, brokerLogger, credentials)
				})
				It("returns a 200", func() {
					response := makeInstanceUpdateRequestWithAcceptsIncomplete(instanceID, brokerapi.UpdateDetails{}, false)
					Expect(response.StatusCode).To(Equal(http.StatusOK))
				})
			})
			Context("when broker can only respond asynchronously", func() {
				BeforeEach(func() {
					fakeAsyncServiceBroker := &fakes.FakeAsyncOnlyServiceBroker{}
					brokerAPI = brokerapi.New(fakeAsyncServiceBroker, brokerLogger, credentials)
				})

				It("returns a 422", func() {
					acceptsIncomplete := false
					response := makeInstanceUpdateRequestWithAcceptsIncomplete(instanceID, brokerapi.UpdateDetails{}, acceptsIncomplete)
					Expect(response.StatusCode).To(Equal(422))
					Expect(response.Body).To(MatchJSON(fixture("async_required.json")))
				})
			})

			Context("when the accepts_incomplete flag is missing", func() {
				BeforeEach(func() {
					fakeAsyncServiceBroker := &fakes.FakeAsyncServiceBroker{}
					brokerAPI = brokerapi.New(fakeAsyncServiceBroker, brokerLogger, credentials)
				})
				It("returns a 200", func() {
					response := makeInstanceUpdateRequest(instanceID, brokerapi.UpdateDetails{}, "")
					Expect(response.StatusCode).To(Equal(http.StatusOK))
				})
			})

			Context("when broker can only respond asynchronously", func() {
				BeforeEach(func() {
					fakeAsyncServiceBroker := &fakes.FakeAsyncOnlyServiceBroker{}
					brokerAPI = brokerapi.New(fakeAsyncServiceBroker, brokerLogger, credentials)
				})

				It("returns a 422", func() {
					acceptsIncomplete := false
					response := makeInstanceUpdateRequestWithAcceptsIncomplete(instanceID, brokerapi.UpdateDetails{}, acceptsIncomplete)
					Expect(response.StatusCode).To(Equal(422))
					Expect(response.Body).To(MatchJSON(fixture("async_required.json")))
				})
			})
		})

		Describe("deprovisioning", func() {
			It("calls Deprovision on the service broker with the instance id", func() {
				instanceID := uniqueInstanceID()
				makeInstanceDeprovisioningRequest(instanceID)
				Expect(fakeServiceBroker.DeprovisionedInstanceIDs).To(ContainElement(instanceID))
			})

			Context("when the instance exists", func() {
				var instanceID string
				var details brokerapi.ProvisionDetails

				BeforeEach(func() {
					instanceID = uniqueInstanceID()
					details = brokerapi.ProvisionDetails{
						PlanID:           "plan-id",
						OrganizationGUID: "organization-guid",
						SpaceGUID:        "space-guid",
					}
					makeInstanceProvisioningRequest(instanceID, details, "")
				})

				It("returns a 200", func() {
					response := makeInstanceDeprovisioningRequest(instanceID)
					Expect(response.StatusCode).To(Equal(200))
				})

				It("returns an empty JSON object", func() {
					response := makeInstanceDeprovisioningRequest(instanceID)
					Expect(response.Body).To(MatchJSON(`{}`))
				})
			})

			Context("when the instance does not exist", func() {
				var instanceID string

				It("returns a 410", func() {
					response := makeInstanceDeprovisioningRequest(uniqueInstanceID())
					Expect(response.StatusCode).To(Equal(410))
				})

				It("returns an empty JSON object", func() {
					response := makeInstanceDeprovisioningRequest(uniqueInstanceID())
					Expect(response.Body).To(MatchJSON(`{}`))
				})

				It("logs an appropriate error", func() {
					instanceID = uniqueInstanceID()
					makeInstanceDeprovisioningRequest(instanceID)
					Expect(lastLogLine().Message).To(ContainSubstring("deprovision.instance-missing"))
					Expect(lastLogLine().Data["error"]).To(ContainSubstring("instance does not exist"))
				})
			})

			Context("when instance deprovisioning fails", func() {
				var instanceID string
				var details brokerapi.ProvisionDetails

				BeforeEach(func() {
					instanceID = uniqueInstanceID()
					details = brokerapi.ProvisionDetails{
						PlanID:           "plan-id",
						OrganizationGUID: "organization-guid",
						SpaceGUID:        "space-guid",
					}
					makeInstanceProvisioningRequest(instanceID, details, "")
				})

				BeforeEach(func() {
					fakeServiceBroker.DeprovisionError = errors.New("broker failed")
				})

				It("returns a 500", func() {
					response := makeInstanceDeprovisioningRequest(instanceID)
					Expect(response.StatusCode).To(Equal(500))
				})

				It("returns json with a description field and a useful error message", func() {
					response := makeInstanceDeprovisioningRequest(instanceID)
					Expect(response.Body).To(MatchJSON(`{"description":"broker failed"}`))
				})

				It("logs an appropriate error", func() {
					makeInstanceDeprovisioningRequest(instanceID)
					Expect(lastLogLine().Message).To(ContainSubstring("provision.unknown-error"))
					Expect(lastLogLine().Data["error"]).To(ContainSubstring("broker failed"))
				})
			})
		})
	})

	Describe("binding lifecycle endpoint", func() {
		makeBindingRequest := func(instanceID, bindingID string, details *brokerapi.BindDetails) *testflight.Response {
			response := &testflight.Response{}
			testflight.WithServer(brokerAPI, func(r *testflight.Requester) {
				path := fmt.Sprintf("/v2/service_instances/%s/service_bindings/%s",
					instanceID, bindingID)

				buffer := &bytes.Buffer{}

				if details != nil {
					json.NewEncoder(buffer).Encode(details)
				}

				request, err := http.NewRequest("PUT", path, buffer)

				Expect(err).NotTo(HaveOccurred())

				request.Header.Add("Content-Type", "application/json")
				request.SetBasicAuth("username", "password")

				response = r.Do(request)
			})
			return response
		}

		Describe("binding", func() {
			var (
				instanceID string
				bindingID  string
				details    *brokerapi.BindDetails
			)

			BeforeEach(func() {
				instanceID = uniqueInstanceID()
				bindingID = uniqueBindingID()
				details = &brokerapi.BindDetails{
					AppGUID:   "app_guid",
					PlanID:    "plan_id",
					ServiceID: "service_id",
				}
			})

			Context("when the associated instance exists", func() {
				It("calls Bind on the service broker with the instance and binding ids", func() {
					makeBindingRequest(instanceID, bindingID, details)
					Expect(fakeServiceBroker.BoundInstanceIDs).To(ContainElement(instanceID))
					Expect(fakeServiceBroker.BoundBindingIDs).To(ContainElement(bindingID))
					Expect(fakeServiceBroker.BoundBindingDetails).To(Equal(*details))
				})

				It("returns the credentials returned by Bind", func() {
					response := makeBindingRequest(uniqueInstanceID(), uniqueBindingID(), details)
					Expect(response.Body).To(MatchJSON(fixture("binding.json")))
				})

				It("returns a 201", func() {
					response := makeBindingRequest(uniqueInstanceID(), uniqueBindingID(), details)
					Expect(response.StatusCode).To(Equal(201))
				})

				Context("when no bind details are being passed", func() {
					It("returns a 422", func() {
						response := makeBindingRequest(uniqueInstanceID(), uniqueBindingID(), nil)
						Expect(response.StatusCode).To(Equal(422))
					})
				})

				Context("when there are arbitrary params", func() {
					BeforeEach(func() {
						details.Parameters = map[string]interface{}{
							"string": "some-string",
							"number": 1,
							"object": struct{ Name string }{"some-name"},
							"array":  []interface{}{"a", "b", "c"},
						}
					})

					It("calls Bind on the service broker with all params", func() {
						makeBindingRequest(instanceID, bindingID, details)
						Expect(fakeServiceBroker.BoundBindingDetails.Parameters["string"]).To(Equal("some-string"))
						Expect(fakeServiceBroker.BoundBindingDetails.Parameters["number"]).To(Equal(1.0))
						Expect(fakeServiceBroker.BoundBindingDetails.Parameters["array"]).To(Equal([]interface{}{"a", "b", "c"}))
						actual, _ := fakeServiceBroker.BoundBindingDetails.Parameters["object"].(map[string]interface{})
						Expect(actual["Name"]).To(Equal("some-name"))
					})
				})
			})

			Context("when the associated instance does not exist", func() {
				var instanceID string

				BeforeEach(func() {
					fakeServiceBroker.BindError = brokerapi.ErrInstanceDoesNotExist
				})

				It("returns a 404", func() {
					response := makeBindingRequest(uniqueInstanceID(), uniqueBindingID(), details)
					Expect(response.StatusCode).To(Equal(404))
				})

				It("returns an error JSON object", func() {
					response := makeBindingRequest(uniqueInstanceID(), uniqueBindingID(), details)
					Expect(response.Body).To(MatchJSON(`{"description":"instance does not exist"}`))
				})

				It("logs an appropriate error", func() {
					instanceID = uniqueInstanceID()
					makeBindingRequest(instanceID, uniqueBindingID(), details)
					Expect(lastLogLine().Message).To(ContainSubstring("bind.instance-missing"))
					Expect(lastLogLine().Data["error"]).To(ContainSubstring("instance does not exist"))
				})
			})

			Context("when the requested binding already exists", func() {
				var instanceID string

				BeforeEach(func() {
					fakeServiceBroker.BindError = brokerapi.ErrBindingAlreadyExists
				})

				It("returns a 409", func() {
					response := makeBindingRequest(uniqueInstanceID(), uniqueBindingID(), details)
					Expect(response.StatusCode).To(Equal(409))
				})

				It("returns an error JSON object", func() {
					response := makeBindingRequest(uniqueInstanceID(), uniqueBindingID(), details)
					Expect(response.Body).To(MatchJSON(`{"description":"binding already exists"}`))
				})

				It("logs an appropriate error", func() {
					instanceID = uniqueInstanceID()
					makeBindingRequest(instanceID, uniqueBindingID(), details)
					makeBindingRequest(instanceID, uniqueBindingID(), details)

					Expect(lastLogLine().Message).To(ContainSubstring("bind.binding-already-exists"))
					Expect(lastLogLine().Data["error"]).To(ContainSubstring("binding already exists"))
				})
			})

			Context("when the binding returns an error", func() {
				BeforeEach(func() {
					fakeServiceBroker.BindError = errors.New("random error")
				})

				It("returns a generic 500 error response", func() {
					response := makeBindingRequest(uniqueInstanceID(), uniqueBindingID(), details)
					Expect(response.StatusCode).To(Equal(500))
					Expect(response.Body).To(MatchJSON(`{"description":"random error"}`))
				})

				It("logs a detailed error message", func() {
					makeBindingRequest(uniqueInstanceID(), uniqueBindingID(), details)

					Expect(lastLogLine().Message).To(ContainSubstring("bind.unknown-error"))
					Expect(lastLogLine().Data["error"]).To(ContainSubstring("random error"))
				})
			})
		})

		Describe("unbinding", func() {
			makeUnbindingRequest := func(instanceID string, bindingID string) *testflight.Response {
				response := &testflight.Response{}
				testflight.WithServer(brokerAPI, func(r *testflight.Requester) {
					path := fmt.Sprintf("/v2/service_instances/%s/service_bindings/%s",
						instanceID, bindingID)
					request, _ := http.NewRequest("DELETE", path, strings.NewReader(""))
					request.Header.Add("Content-Type", "application/json")
					request.SetBasicAuth("username", "password")

					response = r.Do(request)
				})
				return response
			}

			Context("when the associated instance exists", func() {
				var instanceID string
				var details brokerapi.ProvisionDetails

				BeforeEach(func() {
					instanceID = uniqueInstanceID()
					details = brokerapi.ProvisionDetails{
						PlanID:           "plan-id",
						OrganizationGUID: "organization-guid",
						SpaceGUID:        "space-guid",
					}
					makeInstanceProvisioningRequest(instanceID, details, "")
				})

				Context("and the binding exists", func() {
					var bindingID string

					BeforeEach(func() {
						bindingID = uniqueBindingID()
						makeBindingRequest(instanceID, bindingID, &brokerapi.BindDetails{})
					})

					It("returns a 200", func() {
						response := makeUnbindingRequest(instanceID, bindingID)
						Expect(response.StatusCode).To(Equal(200))
					})

					It("returns an empty JSON object", func() {
						response := makeUnbindingRequest(instanceID, bindingID)
						Expect(response.Body).To(MatchJSON(`{}`))
					})
				})

				Context("but the binding does not exist", func() {
					It("returns a 410", func() {
						response := makeUnbindingRequest(instanceID, "does-not-exist")
						Expect(response.StatusCode).To(Equal(410))
					})

					It("logs an appropriate error message", func() {
						makeUnbindingRequest(instanceID, "does-not-exist")

						Expect(lastLogLine().Message).To(ContainSubstring("bind.binding-missing"))
						Expect(lastLogLine().Data["error"]).To(ContainSubstring("binding does not exist"))
					})
				})
			})

			Context("when the associated instance does not exist", func() {
				var instanceID string

				It("returns a 404", func() {
					response := makeUnbindingRequest(uniqueInstanceID(), uniqueBindingID())
					Expect(response.StatusCode).To(Equal(404))
				})

				It("returns an empty JSON object", func() {
					response := makeUnbindingRequest(uniqueInstanceID(), uniqueBindingID())
					Expect(response.Body).To(MatchJSON(`{}`))
				})

				It("logs an appropriate error", func() {
					instanceID = uniqueInstanceID()
					makeUnbindingRequest(instanceID, uniqueBindingID())

					Expect(lastLogLine().Message).To(ContainSubstring("bind.instance-missing"))
					Expect(lastLogLine().Data["error"]).To(ContainSubstring("instance does not exist"))
				})
			})
		})

		Describe("last_operation", func() {
			makeLastOperationRequest := func(instanceID string) *testflight.Response {
				response := &testflight.Response{}
				testflight.WithServer(brokerAPI, func(r *testflight.Requester) {
					path := fmt.Sprintf("/v2/service_instances/%s/last_operation", instanceID)
					request, _ := http.NewRequest("GET", path, strings.NewReader(""))
					request.Header.Add("Content-Type", "application/json")
					request.SetBasicAuth("username", "password")

					response = r.Do(request)
				})
				return response
			}

			It("should return succeeded if the operation completed successfully", func() {
				fakeServiceBroker.LastOperationState = "succeeded"
				fakeServiceBroker.LastOperationDescription = "some description"

				instanceID := "instanceID"
				response := makeLastOperationRequest(instanceID)

				logs := brokerLogger.Logs()

				Expect(logs[0].Message).To(ContainSubstring("lastOperation.starting-check-for-operation"))
				Expect(logs[0].Data["instance-id"]).To(ContainSubstring(instanceID))

				Expect(logs[1].Message).To(ContainSubstring("lastOperation.done-check-for-operation"))
				Expect(logs[1].Data["instance-id"]).To(ContainSubstring(instanceID))
				Expect(logs[1].Data["state"]).To(ContainSubstring(string(fakeServiceBroker.LastOperationState)))

				Expect(response.StatusCode).To(Equal(200))
				Expect(response.Body).To(MatchJSON(fixture("last_operation_succeeded.json")))
			})

			It("should return a 404 and log in case the instance id is not found", func() {
				fakeServiceBroker.LastOperationError = brokerapi.ErrInstanceDoesNotExist
				instanceID := "non-existing"
				response := makeLastOperationRequest(instanceID)

				Expect(lastLogLine().Message).To(ContainSubstring("lastOperation.instance-missing"))
				Expect(lastLogLine().Data["error"]).To(ContainSubstring("instance does not exist"))

				Expect(response.StatusCode).To(Equal(404))
				Expect(response.Body).To(MatchJSON(`{"description": "instance does not exist"}`))
			})

			It("should return an internal sever error for all other errors", func() {
				fakeServiceBroker.LastOperationError = errors.New("Blah")
				response := makeLastOperationRequest("instanceID")

				Expect(lastLogLine().Message).To(ContainSubstring("lastOperation.unknown-error"))
				Expect(lastLogLine().Data["error"]).To(ContainSubstring("Blah"))

				Expect(response.StatusCode).To(Equal(500))
				Expect(response.Body).To(MatchJSON(`{"description": "Blah"}`))
			})
		})
	})
})
