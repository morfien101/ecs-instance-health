package ec2metadatareader

import (
	"fmt"
	"io/ioutil"
	"net/http"
)

const (
	metadataPrefix = "http://169.254.169.254/latest/meta-data"
)

// InstanceID will attempt to determine the instance ID from the metadata service.
func InstanceID() (string, error) {
	resp, err := http.Get(fmt.Sprintf("%s/instance-id", metadataPrefix))
	if err != nil {
		return "", err
	}

	if resp.StatusCode == 200 {
		body, err := ioutil.ReadAll(resp.Body)
		if err != nil {
			return "", fmt.Errorf("Failed to read the metadata response by while getting the instance id. Error: %s", err)
		}
		return string(body), nil
	}

	return "", fmt.Errorf("Could not get the instance id from the meta-data service. Status code of the response: %d", resp.StatusCode)
}

// Region tries to get the region from the availability zone in the meta data service
func Region() (string, error) {
	resp, err := http.Get(fmt.Sprintf("%s/placement/availability-zone", metadataPrefix))
	if err != nil {
		return "", fmt.Errorf("AWS_REGION is not set. Attempted guess failed. Error: %s", err)
	}

	if resp.StatusCode == 200 {
		body, err := ioutil.ReadAll(resp.Body)
		if err != nil {
			return "", fmt.Errorf("Failed to read the metadata response by while getting the region. Error: %s", err)
		}
		return string(body[:len(body)-1]), nil
	}

	return "", fmt.Errorf("Could not get the instance region from the meta-data service. Status code of the response: %d", resp.StatusCode)
}
