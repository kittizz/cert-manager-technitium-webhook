package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	extapi "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"k8s.io/klog/v2"
)

// HTTPClient เป็น HTTP client ที่ใช้สำหรับการเชื่อมต่อกับ API
var HTTPClient = &http.Client{
	Timeout: 30 * time.Second,
	Transport: &http.Transport{
		MaxIdleConns:        10,
		IdleConnTimeout:     30 * time.Second,
		DisableCompression:  true,
		TLSHandshakeTimeout: 10 * time.Second,
	},
}

// technitiumConnector จัดการการเชื่อมต่อและส่งคำขอไปยัง Technitium DNS API
type technitiumConnector struct {
	serverURL string
	authToken string
}

// newTechnitiumConnector สร้าง connector ใหม่สำหรับติดต่อกับ Technitium DNS API
func newTechnitiumConnector(serverURL, token string) *technitiumConnector {
	return &technitiumConnector{
		serverURL: serverURL,
		authToken: token,
	}
}

// ฟังก์ชั่นค้นหา zone ที่เหมาะสมกับ domain
func (c *technitiumConnector) findAuthoritativeZone(fqdn string) (string, error) {
	// เอา trailing dot ออกถ้ามี
	domain := strings.TrimSuffix(fqdn, ".")

	// แยก domain ออกเป็นส่วนๆ แล้วค่อยๆตัดจากซ้ายทีละส่วน
	parts := strings.Split(domain, ".")

	for i := 0; i < len(parts); i++ {
		// สร้าง subdomain โดยเริ่มจากการตัดทิ้ง level ซ้ายสุดก่อน
		checkZone := strings.Join(parts[i:], ".")
		klog.V(4).Infof("Checking if %s is an authoritative zone", checkZone)

		// ตรวจสอบว่า zone นี้มีอยู่จริงใน Technitium DNS Server หรือไม่
		endpoint := fmt.Sprintf("%s/api/zones/records/get?token=%s&domain=%s&listZone=false",
			c.serverURL, url.QueryEscape(c.authToken), url.QueryEscape(checkZone))

		resp, err := HTTPClient.Get(endpoint)
		if err != nil {
			klog.V(4).Infof("Error querying zone %s: %v", checkZone, err)
			continue
		}

		defer resp.Body.Close()
		body, err := io.ReadAll(resp.Body)
		if err != nil {
			klog.V(4).Infof("Error reading response for zone %s: %v", checkZone, err)
			continue
		}

		// ตรวจสอบว่าการเรียก API สำเร็จหรือไม่
		var response struct {
			Status   string `json:"status"`
			Response struct {
				Zone struct {
					Name         string `json:"name"`
					Type         string `json:"type"`
					DnssecStatus string `json:"dnssecStatus"`
					Disabled     bool   `json:"disabled"`
				} `json:"zone"`
			} `json:"response"`
		}

		if err := json.Unmarshal(body, &response); err != nil {
			klog.V(4).Infof("Error parsing response for zone %s: %v", checkZone, err)
			continue
		}

		klog.V(4).Infof("Zone check for %s status: %s", checkZone, response.Status)
		if response.Status == "ok" && !response.Response.Zone.Disabled {
			return checkZone, nil
		}
	}

	return "", fmt.Errorf("no authoritative zone found for domain %s", fqdn)
}

// สร้าง TXT record
func (c *technitiumConnector) createTXTRecord(zone, fqdn, value string, ttl int) error {
	// เอา trailing dot ออกถ้ามี
	domain := strings.TrimSuffix(fqdn, ".")
	zone = strings.TrimSuffix(zone, ".")

	klog.Infof("Creating TXT record for %s with value %s (zone: %s, ttl: %d)", domain, value, zone, ttl)

	// สร้าง URL สำหรับเรียก API
	endpoint := fmt.Sprintf("%s/api/zones/records/add", c.serverURL)

	// สร้างพารามิเตอร์สำหรับ HTTP request
	data := url.Values{}
	data.Set("token", c.authToken)
	data.Set("domain", domain)
	data.Set("zone", zone)
	data.Set("type", "TXT")
	data.Set("ttl", fmt.Sprintf("%d", ttl))
	data.Set("text", value)
	data.Set("splitText", "false")

	// ส่ง HTTP request
	resp, err := HTTPClient.PostForm(endpoint, data)
	if err != nil {
		klog.Errorf("HTTP request failed: %v", err)
		return err
	}
	defer resp.Body.Close()

	// อ่านผลลัพธ์
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		klog.Errorf("Failed to read response body: %v", err)
		return err
	}

	// ตรวจสอบว่าการเรียก API สำเร็จหรือไม่
	var response struct {
		Status       string `json:"status"`
		ErrorMessage string `json:"errorMessage,omitempty"`
	}

	if err := json.Unmarshal(body, &response); err != nil {
		klog.Errorf("Failed to parse API response: %v", err)
		return fmt.Errorf("error parsing API response: %v", err)
	}

	if response.Status != "ok" {
		klog.Errorf("API error response: %s", string(body))
		return fmt.Errorf("API error: %s", response.ErrorMessage)
	}

	klog.Infof("Successfully created TXT record for %s", domain)
	return nil
}

// ลบ TXT record
func (c *technitiumConnector) deleteTXTRecord(zone, fqdn, value string) error {
	// เอา trailing dot ออกถ้ามี
	domain := strings.TrimSuffix(fqdn, ".")
	zone = strings.TrimSuffix(zone, ".")

	klog.Infof("Deleting TXT record for %s with value %s (zone: %s)", domain, value, zone)

	// สร้าง URL สำหรับเรียก API
	endpoint := fmt.Sprintf("%s/api/zones/records/delete", c.serverURL)

	// สร้างพารามิเตอร์สำหรับ HTTP request
	data := url.Values{}
	data.Set("token", c.authToken)
	data.Set("domain", domain)
	data.Set("zone", zone)
	data.Set("type", "TXT")
	data.Set("text", value)
	data.Set("splitText", "false")

	fmt.Println("deleteTXTRecord:", data)
	// ส่ง HTTP request
	resp, err := HTTPClient.PostForm(endpoint, data)
	if err != nil {
		klog.Errorf("HTTP request failed: %v", err)
		return err
	}
	defer resp.Body.Close()

	// อ่านผลลัพธ์
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		klog.Errorf("Failed to read response body: %v", err)
		return err
	}

	// ตรวจสอบว่าการเรียก API สำเร็จหรือไม่
	var response struct {
		Status       string `json:"status"`
		ErrorMessage string `json:"errorMessage,omitempty"`
	}

	if err := json.Unmarshal(body, &response); err != nil {
		klog.Errorf("Failed to parse API response: %v", err)
		return fmt.Errorf("error parsing API response: %v", err)
	}

	if response.Status != "ok" {
		// ในกรณีที่ไม่พบ record อาจเป็นเพราะถูกลบไปแล้ว ให้บันทึก warning แต่ไม่ถือว่าเป็นข้อผิดพลาด
		klog.Warningf("API returned non-ok status: %s, error: %s", response.Status, response.ErrorMessage)
		if strings.Contains(strings.ToLower(response.ErrorMessage), "not found") {
			klog.Infof("Record might already be deleted, continuing")
			return nil
		}
		return fmt.Errorf("API error: %s", response.ErrorMessage)
	}

	klog.Infof("Successfully deleted TXT record for %s", domain)
	return nil
}

// // getTXTRecords ดึงข้อมูล TXT records ทั้งหมดของ domain
// func (c *technitiumConnector) getTXTRecords(fqdn string) ([]string, error) {
// 	// เอา trailing dot ออกถ้ามี
// 	domain := strings.TrimSuffix(fqdn, ".")

// 	klog.V(4).Infof("Getting TXT records for %s", domain)

// 	// สร้าง URL สำหรับเรียก API
// 	endpoint := fmt.Sprintf("%s/api/dns/query?token=%s&name=%s&type=TXT", c.serverURL, url.QueryEscape(c.authToken), url.QueryEscape(domain))

// 	// ส่ง HTTP request
// 	resp, err := HTTPClient.Get(endpoint)
// 	if err != nil {
// 		klog.V(4).Infof("HTTP request failed: %v", err)
// 		return nil, err
// 	}
// 	defer resp.Body.Close()

// 	// อ่านผลลัพธ์
// 	body, err := io.ReadAll(resp.Body)
// 	if err != nil {
// 		klog.V(4).Infof("Failed to read response body: %v", err)
// 		return nil, err
// 	}

// 	// ตรวจสอบว่าการเรียก API สำเร็จหรือไม่
// 	var response struct {
// 		Status   string `json:"status"`
// 		Response struct {
// 			Answer []struct {
// 				Name  string `json:"name"`
// 				Type  string `json:"type"`
// 				TTL   int    `json:"ttl"`
// 				Value string `json:"value"`
// 			} `json:"answer"`
// 		} `json:"response"`
// 	}

// 	if err := json.Unmarshal(body, &response); err != nil {
// 		klog.V(4).Infof("Failed to parse API response: %v", err)
// 		return nil, fmt.Errorf("error parsing API response: %v", err)
// 	}

// 	if response.Status != "ok" {
// 		klog.V(4).Infof("API returned non-ok status: %s", response.Status)
// 		return nil, fmt.Errorf("API error")
// 	}

// 	var records []string
// 	for _, answer := range response.Response.Answer {
// 		if answer.Type == "TXT" {
// 			// TXT records มาในรูปแบบที่มีเครื่องหมาย quote ล้อมรอบ จึงต้องนำออก
// 			value := strings.Trim(answer.Value, "\"")
// 			records = append(records, value)
// 			klog.V(4).Infof("Found TXT record: %s", value)
// 		}
// 	}

// 	return records, nil
// }

// loadConfig แปลงค่า JSON เป็นโครงสร้าง config
func loadConfig(cfgJSON *extapi.JSON) (technitiumDNSProviderConfig, error) {
	cfg := technitiumDNSProviderConfig{}
	// จัดการกรณีที่ไม่มีการตั้งค่า
	if cfgJSON == nil {
		return cfg, fmt.Errorf("no config provided")
	}

	if err := json.Unmarshal(cfgJSON.Raw, &cfg); err != nil {
		return cfg, fmt.Errorf("error decoding solver config: %v", err)
	}

	// ตรวจสอบข้อมูลที่จำเป็น
	if cfg.ServerURL == "" {
		return cfg, fmt.Errorf("serverUrl must be provided")
	}

	if cfg.AuthTokenSecretRef.LocalObjectReference.Name == "" || cfg.AuthTokenSecretRef.Key == "" {
		return cfg, fmt.Errorf("authTokenSecretRef must be provided")
	}

	return cfg, nil
}
