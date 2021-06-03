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

package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/spf13/pflag"
	"k8s.io/klog"

	"github.com/virtual-kubelet/tensile-kube/cmd/webhook/app"
)

func main() {
	klog.InitFlags(nil)
	options := app.NewServerRunOptions()
	pflag.CommandLine.AddGoFlagSet(flag.CommandLine)
	pflag.Parse()
	if options.ShowVersion {
		fmt.Println(os.Args[0], app.Version)
		return
	}

	options.SetDefaults()
	if err := options.Validate(); err != nil {
		fmt.Fprintf(os.Stderr, "%v\n", err)
		os.Exit(1)
	}

	klog.Infof("starting webhook server.")
	if err := app.Run(options); err != nil {
		fmt.Fprintf(os.Stderr, "%v\n", err)
		os.Exit(1)
	}
}
