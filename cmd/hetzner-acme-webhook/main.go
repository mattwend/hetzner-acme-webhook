// SPDX-FileCopyrightText: 2026 Matthias Wende
// SPDX-License-Identifier: GPL-3.0-only

package main

import (
	"github.com/cert-manager/cert-manager/pkg/acme/webhook/cmd"
	"github.com/mattwend/hetzner-acme-webhook/internal/webhook"
	"k8s.io/klog/v2"
)

func main() {
	klog.InitFlags(nil)
	defer klog.Flush()

	client, err := webhook.NewDNSClient()
	if err != nil {
		klog.Fatalf("configure client: %v", err)
	}

	zone, err := webhook.ZoneFromEnv()
	if err != nil {
		klog.Infof("health checks will run in local-only mode: %v", err)
		zone = ""
	}

	go webhook.ServeHealth(client, zone)

	cmd.RunWebhookServer(webhook.GroupName, webhook.NewSolver(client))
}
