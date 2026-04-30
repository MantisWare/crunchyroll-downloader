package main

import (
	"net/http"
)

func DoRequest(req *http.Request) (*http.Response, error) {
	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode == http.StatusUnauthorized {
		print("Access token expired. Refetching one...\n")
		// Refetch an access token
		token = GetAccessToken(*etpRt)
		req.Header.Set("Authorization", "Bearer "+token)
		// and retry the request
		return DoRequest(req)
	}

	return resp, err
}
