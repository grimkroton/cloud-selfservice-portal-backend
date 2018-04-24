package openshift

import (
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strings"

	"github.com/Jeffail/gabs"
	"github.com/gin-gonic/gin"
	"github.com/oscp/cloud-selfservice-portal-backend/server/common"
)

const (
	genericAPIError         = "Fehler beim Aufruf der OpenShift-API. Bitte erstelle ein Ticket"
	wrongAPIUsageError      = "Invalid api call - parameters did not match to method definition"
	testProjectDeletionDays = "30"
)

// RegisterRoutes registers the routes for OpenShift
func RegisterRoutes(r *gin.RouterGroup) {
	// OpenShift
	r.POST("/ose/project", newProjectHandler)
	r.GET("/ose/project/:project/admins", getProjectAdminsHandler)
	r.POST("/ose/testproject", newTestProjectHandler)
	r.POST("/ose/serviceaccount", newServiceAccountHandler)
	r.GET("/ose/billing/:project", getBillingHandler)
	r.POST("/ose/billing", updateBillingHandler)
	r.POST("/ose/quotas", editQuotasHandler)

	// GlusterFS
	r.POST("/gluster/volume", newVolumeHandler)
	r.POST("/gluster/volume/fix", fixVolumeHandler)
	r.POST("/gluster/volume/grow", growVolumeHandler)
}

func RegisterSecRoutes(r *gin.RouterGroup) {
	r.POST("/gluster/volume/fix", fixVolumeHandler)
}

func getProjectAdminsAndOperators(project string) ([]string, []string, error) {
	policyBindings, err := getPolicyBindings(project)
	if err != nil {
		return nil, nil, err
	}

	children, err := policyBindings.S("roleBindings").Children()
	if err != nil {
		log.Println("Unable to parse roleBindings", err.Error())
		return nil, nil, errors.New(genericAPIError)
	}

	var admins []string
	hasOperatorGroup := false
	for _, v := range children {
		if v.Path("name").Data().(string) == "admin" {
			groups, err := v.Path("roleBinding.groupNames").Children()
			if err == nil {
				for _, g := range groups {
					if strings.ToLower(g.Data().(string)) == "operator" {
						hasOperatorGroup = true
					}
				}
			}
			usernames, err := v.Path("roleBinding.userNames").Children()
			if err != nil {
				log.Println("Unable to parse roleBinding", err.Error())
				return nil, nil, errors.New(genericAPIError)
			}
			for _, u := range usernames {
				admins = append(admins, strings.ToLower(u.Data().(string)))
			}
		}
	}

	var operators []string
	if hasOperatorGroup {
		// Going to add the operator group to the admins
		json, err := getOperatorGroup()
		if err != nil {
			return nil, nil, err
		}

		users, err := json.Path("users").Children()
		if err != nil {
			log.Println("Could not parse operator group:", json, err.Error())
			return nil, nil, errors.New(genericAPIError)
		}

		for _, u := range users {
			operators = append(operators, strings.ToLower(u.Data().(string)))
		}
	}

	return admins, operators, nil
}

func checkAdminPermissions(username string, project string) error {
	// Check if user has admin-access
	hasAccess := false
	admins, operators, err := getProjectAdminsAndOperators(project)
	if err != nil {
		return err
	}

	username = strings.ToLower(username)

	// Access for admins
	for _, a := range admins {
		if username == a {
			hasAccess = true
		}
	}

	// Access for operators
	for _, o := range operators {
		if username == o {
			hasAccess = true
		}
	}

	if hasAccess {
		return nil
	}

	return fmt.Errorf("Du hast keine Admin Rechte auf dem Projekt. Bestehende Admins sind folgende Benutzer: %v", strings.Join(admins, ", "))
}

func getOperatorGroup() (*gabs.Container, error) {
	client, req := getOseHTTPClient("GET", "oapi/v1/groups/operator", nil)
	resp, err := client.Do(req)

	if err != nil {
		log.Println("Error from OpenShift API: ", err.Error())
		return nil, errors.New(genericAPIError)
	}

	defer resp.Body.Close()

	json, err := gabs.ParseJSONBuffer(resp.Body)
	if err != nil {
		log.Println("error parsing body of response:", err)
		return nil, errors.New(genericAPIError)
	}

	return json, nil
}

func getPolicyBindings(project string) (*gabs.Container, error) {
	client, req := getOseHTTPClient("GET", "oapi/v1/namespaces/"+project+"/policybindings/:default", nil)
	resp, err := client.Do(req)

	if err != nil {
		log.Println("Error from OpenShift API: ", err.Error())
		return nil, errors.New(genericAPIError)
	}

	defer resp.Body.Close()

	if resp.StatusCode == 404 {
		log.Println("Project was not found", project)
		return nil, errors.New("Das Projekt existiert nicht")
	}

	json, err := gabs.ParseJSONBuffer(resp.Body)
	if err != nil {
		log.Println("error parsing body of response:", err)
		return nil, errors.New(genericAPIError)
	}

	return json, nil
}

func getOseAddress(end string) string {
	base := os.Getenv("OPENSHIFT_API")

	if len(base) == 0 {
		log.Fatal("Env variable 'OPENSHIFT_API' must be specified")
	}

	return base + "/" + end
}

func getOseHTTPClient(method string, endURL string, body io.Reader) (*http.Client, *http.Request) {
	token := os.Getenv("OPENSHIFT_TOKEN")
	if len(token) == 0 {
		log.Fatal("Env variable 'OPENSHIFT_TOKEN' must be specified")
	}

	tr := &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
	}
	client := &http.Client{Transport: tr}

	req, _ := http.NewRequest(method, getOseAddress(endURL), body)

	if common.DebugMode() {
		log.Print("Calling ", req.URL.String())
	}

	req.Header.Add("Authorization", "Bearer "+token)

	return client, req
}

func getGlusterHTTPClient(url string, body io.Reader) (*http.Client, *http.Request) {
	apiUrl := os.Getenv("GLUSTER_API_URL")
	apiSecret := os.Getenv("GLUSTER_SECRET")

	if len(apiUrl) == 0 || len(apiSecret) == 0 {
		log.Fatal("Env variables 'GLUSTER_API_URL' and 'GLUSTER_SECRET' must be specified")
	}

	client := &http.Client{}
	req, _ := http.NewRequest("POST", fmt.Sprintf("%v/%v", apiUrl, url), body)

	if common.DebugMode() {
		log.Printf("Calling %v", req.URL.String())
	}

	req.SetBasicAuth("GLUSTER_API", apiSecret)

	return client, req
}

func newObjectRequest(kind string, name string) *gabs.Container {
	json := gabs.New()

	json.Set(kind, "kind")
	json.Set("v1", "apiVersion")
	json.SetP(name, "metadata.name")

	return json
}
