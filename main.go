package main

import (
	"context"
	"fmt"
	"os"

	cmmeta "github.com/cert-manager/cert-manager/pkg/apis/meta/v1"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/klog/v2"

	"github.com/cert-manager/cert-manager/pkg/acme/webhook/apis/acme/v1alpha1"
	"github.com/cert-manager/cert-manager/pkg/acme/webhook/cmd"
)

// GroupName คือ DNS provider group ที่ใช้สำหรับ webhook
var GroupName = os.Getenv("GROUP_NAME")

func main() {
	if GroupName == "" {
		panic("GROUP_NAME must be specified")
	}

	cmd.RunWebhookServer(GroupName,
		&technitiumDNSProviderSolver{},
	)
}

// technitiumDNSProviderSolver เป็นตัวที่ implement logic สำหรับการจัดการกับ ACME challenge
type technitiumDNSProviderSolver struct {
	client           *kubernetes.Clientset
	connectorCreator func(serverURL, token string) *technitiumConnector
}

// technitiumDNSProviderConfig โครงสร้างข้อมูลการตั้งค่าที่จำเป็นสำหรับการเชื่อมต่อกับ Technitium DNS API
type technitiumDNSProviderConfig struct {
	// ServerURL คือ URL ของ Technitium DNS Server
	ServerURL string `json:"serverUrl"`

	// AuthTokenSecretRef อ้างอิงไปยัง Secret ที่เก็บ token สำหรับเข้าถึง Technitium DNS API
	AuthTokenSecretRef cmmeta.SecretKeySelector `json:"authTokenSecretRef"`

	// Zone คือชื่อ DNS zone ที่ต้องการเพิ่ม TXT record (ถ้าไม่ระบุจะใช้การค้นหาอัตโนมัติ)
	Zone string `json:"zone,omitempty"`

	// TTL คือค่า TTL ของ TXT record ที่สร้างขึ้น (default: 60)
	TTL int `json:"ttl,omitempty"`
}

// Name คือชื่อของ DNS solver
func (c *technitiumDNSProviderSolver) Name() string {
	return "technitium"
}

// Present สร้าง TXT record ใน Technitium DNS สำหรับทำการตรวจสอบ DNS01 challenge
func (c *technitiumDNSProviderSolver) Present(ch *v1alpha1.ChallengeRequest) error {
	klog.Infof("Presenting challenge for domain %s", ch.ResolvedFQDN)

	connector, zone, ttl, err := c.createConnectorFromChallenge(ch)
	if err != nil {
		return err
	}

	// สร้าง TXT record สำหรับ challenge
	err = connector.createTXTRecord(zone, ch.ResolvedFQDN, ch.Key, ttl)
	if err != nil {
		return fmt.Errorf("error creating TXT record: %v", err)
	}

	klog.Infof("Successfully presented challenge for domain %s", ch.ResolvedFQDN)
	return nil
}

// CleanUp ลบ TXT record ที่ใช้สำหรับการตรวจสอบ DNS01 challenge
func (c *technitiumDNSProviderSolver) CleanUp(ch *v1alpha1.ChallengeRequest) error {
	klog.Infof("Cleaning up challenge for domain %s", ch.ResolvedFQDN)

	connector, zone, _, err := c.createConnectorFromChallenge(ch)
	if err != nil {
		return err
	}

	// ลบ TXT record
	err = connector.deleteTXTRecord(zone, ch.ResolvedFQDN, ch.Key)
	if err != nil {
		return fmt.Errorf("error deleting TXT record: %v", err)
	}

	klog.Infof("Successfully cleaned up challenge for domain %s", ch.ResolvedFQDN)
	return nil
}

// Initialize เริ่มต้นการทำงานของ webhook
func (c *technitiumDNSProviderSolver) Initialize(kubeClientConfig *rest.Config, stopCh <-chan struct{}) error {
	klog.Infof("Initializing Technitium DNS webhook")
	cl, err := kubernetes.NewForConfig(kubeClientConfig)
	if err != nil {
		return err
	}

	c.client = cl
	c.connectorCreator = newTechnitiumConnector
	return nil
}

// createConnectorFromChallenge สร้าง connector และเตรียมข้อมูลที่จำเป็นจากคำขอ challenge
func (c *technitiumDNSProviderSolver) createConnectorFromChallenge(ch *v1alpha1.ChallengeRequest) (*technitiumConnector, string, int, error) {
	cfg, err := loadConfig(ch.Config)
	if err != nil {
		return nil, "", 0, fmt.Errorf("error loading config: %v", err)
	}

	// ดึงค่า token จาก Secret
	authToken, err := c.getTokenFromSecret(cfg, ch.ResourceNamespace)
	if err != nil {
		return nil, "", 0, fmt.Errorf("error getting auth token: %v", err)
	}

	// กำหนดค่า TTL เริ่มต้นถ้าไม่ได้ระบุ
	ttl := 60
	if cfg.TTL > 0 {
		ttl = cfg.TTL
	}

	connector := c.connectorCreator(cfg.ServerURL, authToken)

	// หา zone ที่เหมาะสมถ้าไม่ได้ระบุ
	zone := ch.ResolvedZone
	if zone == "" {
		klog.Infof("Zone not specified, attempting to find authoritative zone for %s", ch.ResolvedFQDN)
		zone, err = connector.findAuthoritativeZone(ch.ResolvedFQDN)
		if err != nil {
			return nil, "", 0, fmt.Errorf("error finding zone for domain %s: %v", ch.ResolvedFQDN, err)
		}
		klog.Infof("Found authoritative zone: %s", zone)
	}

	return connector, zone, ttl, nil
}

// ฟังก์ชั่นสำหรับอ่าน token จาก Kubernetes Secret
func (c *technitiumDNSProviderSolver) getTokenFromSecret(cfg technitiumDNSProviderConfig, namespace string) (string, error) {
	secretName := cfg.AuthTokenSecretRef.LocalObjectReference.Name
	secretKey := cfg.AuthTokenSecretRef.Key

	if secretName == "" || secretKey == "" {
		return "", fmt.Errorf("secret name or key not provided")
	}

	klog.V(4).Infof("Getting auth token from secret %s/%s", namespace, secretName)
	secret, err := c.client.CoreV1().Secrets(namespace).Get(context.TODO(), secretName, metav1.GetOptions{})
	if err != nil {
		return "", fmt.Errorf("error getting secret: %v", err)
	}

	keyBytes, ok := secret.Data[secretKey]
	if !ok {
		return "", fmt.Errorf("key %q not found in secret %q", secretKey, secretName)
	}

	return string(keyBytes), nil
}
