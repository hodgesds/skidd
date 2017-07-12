// Copyright © 2017 Daniel Hodges
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package cmd

import (
	"crypto/rand"
	"crypto/rsa"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"

	"github.com/hodgesds/skidd"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"golang.org/x/crypto/ssh"
)

var cfgFile string
var keyFile string
var sshPort = 22
var wsPort = 9400

// RootCmd represents the base command when called without any subcommands
var RootCmd = &cobra.Command{
	Use:   "skidd",
	Short: "",
	Long:  ``,
	Run: func(cmd *cobra.Command, args []string) {
		privKey, err := rsa.GenerateKey(rand.Reader, 2048)
		if err != nil {
			log.Fatal(err)
		}

		signer, err := ssh.NewSignerFromKey(privKey)
		if err != nil {
			log.Fatal(err)
		}

		serverConfig := &ssh.ServerConfig{
			PasswordCallback: func(
				c ssh.ConnMetadata,
				pass []byte,
			) (*ssh.Permissions, error) {
				// Should use constant-time compare (or better, salt+hash) in
				// a production setting.
				if c.User() == "foo" && string(pass) == "foo" {
					return nil, nil
				}
				return nil, fmt.Errorf("password rejected for %q", c.User())
			},
		}
		serverConfig.AddHostKey(signer)

		serverConfig.NoClientAuth = true

		cm := skidd.NewConnMaster(serverConfig)

		wsCm := skidd.NewWsConnMaster(cm)

		http.HandleFunc("/", wsCm.WsHandler)

		go func() {
			log.Printf("listening on %d for websocket connections\n", wsPort)
			log.Fatal(http.ListenAndServe(
				fmt.Sprintf("0.0.0.0:%d", wsPort),
				nil),
			)
		}()

		fmt.Printf("listening on %d for ssh connections\n", sshPort)
		listener, err := net.Listen("tcp", fmt.Sprintf("0.0.0.0:%d", sshPort))
		if err != nil {
			log.Fatal(err)
		}

		for {
			conn, err := listener.Accept()
			if err != nil {
				log.Fatal(err)
			}
			cm.HandleConn(conn)
		}
	},
}

// Execute adds all child commands to the root command sets flags appropriately.
// This is called by main.main(). It only needs to happen once to the rootCmd.
func Execute() {
	if err := RootCmd.Execute(); err != nil {
		fmt.Println(err)
		os.Exit(-1)
	}
}

func init() {
	cobra.OnInitialize(initConfig)

	RootCmd.PersistentFlags().StringVar(
		&cfgFile,
		"config", "",
		"config file (default is $HOME/.skidd.yaml)",
	)

	RootCmd.Flags().IntVarP(&sshPort, "ssh-port", "p", 22, "Port")
	RootCmd.Flags().IntVarP(&wsPort, "ws-port", "w", 9400, "Port")

	RootCmd.Flags().StringVarP(&keyFile, "key", "k", "", "Key file")
}

// initConfig reads in config file and ENV variables if set.
func initConfig() {
	if cfgFile != "" { // enable ability to specify config file via flag
		viper.SetConfigFile(cfgFile)
	}

	viper.SetConfigName(".skidd") // name of config file (without extension)
	viper.AddConfigPath("$HOME")  // adding home directory as first search path
	viper.AutomaticEnv()          // read in environment variables that match

	// If a config file is found, read it in.
	if err := viper.ReadInConfig(); err == nil {
		fmt.Println("Using config file:", viper.ConfigFileUsed())
	}
}
