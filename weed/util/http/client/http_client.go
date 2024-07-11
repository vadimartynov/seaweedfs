package client

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	util "github.com/seaweedfs/seaweedfs/weed/util"
	"github.com/spf13/viper"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
)

var (
	onceLoadSecurityConfiguration sync.Once
)

type HTTPClient struct {
	Client            *http.Client
	Transport         *http.Transport
	expectHttpsScheme bool
}

func (httpClient *HTTPClient) Do(req *http.Request) (*http.Response, error) {
	req.URL.Scheme = httpClient.GetHttpScheme()
	return httpClient.Client.Do(req)
}

func (httpClient *HTTPClient) Get(url string) (resp *http.Response, err error) {
	url, err = httpClient.NormalizeHttpScheme(url)
	if err != nil {
		return nil, err
	}
	return httpClient.Client.Get(url)
}

func (httpClient *HTTPClient) Post(url, contentType string, body io.Reader) (resp *http.Response, err error) {
	url, err = httpClient.NormalizeHttpScheme(url)
	if err != nil {
		return nil, err
	}
	return httpClient.Client.Post(url, contentType, body)
}

func (httpClient *HTTPClient) PostForm(url string, data url.Values) (resp *http.Response, err error) {
	url, err = httpClient.NormalizeHttpScheme(url)
	if err != nil {
		return nil, err
	}
	return httpClient.Client.PostForm(url, data)
}

func (httpClient *HTTPClient) Head(url string) (resp *http.Response, err error) {
	url, err = httpClient.NormalizeHttpScheme(url)
	if err != nil {
		return nil, err
	}
	return httpClient.Client.Head(url)
}
func (httpClient *HTTPClient) CloseIdleConnections() {
	httpClient.Client.CloseIdleConnections()
}

func (httpClient *HTTPClient) GetClientTransport() *http.Transport {
	return httpClient.Transport
}

func (httpClient *HTTPClient) GetHttpScheme() string {
	if httpClient.expectHttpsScheme {
		return "https"
	}
	return "http"
}

func (httpClient *HTTPClient) NormalizeHttpScheme(rawURL string) (string, error) {
	expectedScheme := httpClient.GetHttpScheme()

	if !(strings.HasPrefix(rawURL, "http://") || strings.HasPrefix(rawURL, "https://")) {
		return expectedScheme + "://" + rawURL, nil
	}

	parsedURL, err := url.Parse(rawURL)
	if err != nil {
		return "", err
	}

	if expectedScheme != parsedURL.Scheme {
		parsedURL.Scheme = expectedScheme
	}
	return parsedURL.String(), nil
}

func NewHttpClient(serviceOrClientName ClientName, opts ...HttpClientOpt) (*HTTPClient, error) {
	httpClient := HTTPClient{}
	httpClient.expectHttpsScheme = false

	clientCertPair, err := GetClientCertPair(serviceOrClientName)
	if err != nil {
		return nil, err
	}

	clientCaCert, clientCaCertName, err := GetClientCaCert(serviceOrClientName)
	if err != nil {
		return nil, err
	}

	var tlsConfig *tls.Config = nil
	if clientCertPair != nil || len(clientCaCert) != 0 {
		httpClient.expectHttpsScheme = true
		caCertPool, err := CreateHTTPClientCertPool(clientCaCert, clientCaCertName)
		if err != nil {
			return nil, err
		}

		tlsConfig = &tls.Config{
			Certificates:       []tls.Certificate{},
			RootCAs:            caCertPool,
			InsecureSkipVerify: false,
		}

		if clientCertPair != nil {
			tlsConfig.Certificates = append(tlsConfig.Certificates, *clientCertPair)
		}
	}

	httpClient.Transport = &http.Transport{
		MaxIdleConns:        1024,
		MaxIdleConnsPerHost: 1024,
		TLSClientConfig:     tlsConfig,
	}
	httpClient.Client = &http.Client{
		Transport: httpClient.Transport,
	}

	for _, opt := range opts {
		opt(&httpClient)
	}
	return &httpClient, nil
}

func GetFileNameFromSecurityConfiguration(serviceOrClientName ClientName, fileType string) string {
	onceLoadSecurityConfiguration.Do(func() {
		util.LoadConfiguration("security", false)
	})
	return viper.GetString(fmt.Sprintf("https.%s.%s", serviceOrClientName.LowerCaseString(), fileType))
}

func GetFileContentFromSecurityConfiguration(serviceOrClientName ClientName, fileType string) ([]byte, string, error) {
	if fileName := GetFileNameFromSecurityConfiguration(serviceOrClientName, fileType); fileName != "" {
		fileContent, err := os.ReadFile(fileName)
		if err != nil {
			return nil, fileName, err
		}
		return fileContent, fileName, err
	}
	return nil, "", nil
}

func GetClientCertPair(serviceOrClientName ClientName) (*tls.Certificate, error) {
	certFileName := GetFileNameFromSecurityConfiguration(serviceOrClientName, "cert")
	keyFileName := GetFileNameFromSecurityConfiguration(serviceOrClientName, "key")
	if certFileName == "" && keyFileName == "" {
		return nil, nil
	}
	if certFileName != "" && keyFileName != "" {
		clientCert, err := tls.LoadX509KeyPair(certFileName, keyFileName)
		if err != nil {
			return nil, fmt.Errorf("error loading client certificate and key: %s", err)
		}
		return &clientCert, nil
	}
	return nil, fmt.Errorf("error loading key pair: key `%s` and certificate `%s`", keyFileName, certFileName)
}

func GetClientCaCert(serviceOrClientName ClientName) ([]byte, string, error) {
	return GetFileContentFromSecurityConfiguration(serviceOrClientName, "ca")
}

func CreateHTTPClientCertPool(certContent []byte, fileName string) (*x509.CertPool, error) {
	certPool := x509.NewCertPool()
	if len(certContent) == 0 {
		return certPool, nil
	}

	ok := certPool.AppendCertsFromPEM(certContent)
	if !ok {
		return nil, fmt.Errorf("error processing certificate in %s", fileName)
	}
	return certPool, nil
}