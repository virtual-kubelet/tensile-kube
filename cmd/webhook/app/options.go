/*
 * Copyright ©2020. The virtual-kubelet authors
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

package app

import (
	"fmt"
	"net"

	"github.com/spf13/pflag"
	"github.com/virtual-kubelet/tensile-kube/pkg/util"
)

var (
	// Version is used for support printing version
	Version = "unknown"
)

// ServerRunOptions defines the options of webhook server
type ServerRunOptions struct {
	// webhook listen address
	Address string
	// listen port
	Port int
	// ca of tls
	TLSCA string
	// cert of tls
	TLSCert string
	// key of tls
	TLSKey string
	// kubeconfig file path if running out of cluster
	Kubeconfig string
	// url of master
	MasterURL string
	// run in the k8s
	InCluster bool
	// ignoreSelectorKeys represents those nodeSelector keys should not be converted
	// and it would affect the scheduling in then upper cluster
	IgnoreSelectorKeys string
	// ShowVersion is used for version
	ShowVersion bool
}

// NewServerRunOptions returns the run options
func NewServerRunOptions() *ServerRunOptions {
	options := &ServerRunOptions{}
	options.addFlags()
	return options
}

func (s *ServerRunOptions) addFlags() {
	pflag.StringVar(&s.Address, "address", "0.0.0.0", "The address of scheduler manager.")
	pflag.IntVar(&s.Port, "port", 8080, "The port of scheduler manager.")
	pflag.StringVar(&s.TLSCert, "tlscert", "", "Path to TLS certificate file")
	pflag.StringVar(&s.TLSKey, "tlskey", "", "Path to TLS key file")
	pflag.StringVar(&s.TLSCA, "CA", "", "Path to certificate file")
	pflag.StringVar(&s.Kubeconfig, "kubeconfig", "", "Absolute path to the kubeconfig file.")
	pflag.StringVar(&s.MasterURL, "master", "", "Master url.")
	pflag.BoolVar(&s.InCluster, "incluster", false, "If this extender running in the cluster.")
	pflag.StringVar(&s.IgnoreSelectorKeys, "ignore-selector-keys", util.ClusterID,
		"IgnoreSelectorKeys represents those nodeSelector keys should not be converted, "+
			"it would affect the scheduling in then upper cluster, multi values should split by comma(,)")
	pflag.BoolVar(&s.ShowVersion, "version", false, "Show version.")
}

// Validate is used for validate address
func (s *ServerRunOptions) Validate() error {
	address := net.ParseIP(s.Address)
	if address.To4() == nil {
		return fmt.Errorf("%v is not a valid IP address", s.Address)
	}
	return nil
}
