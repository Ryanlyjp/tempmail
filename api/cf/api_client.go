package cf

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const baseURL = "https://api.cloudflare.com/client/v4"

type Client struct {
	Token string
	HTTP  *http.Client
}

type Zone struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type DNSRecord struct {
	ID       string `json:"id"`
	Type     string `json:"type"`
	Name     string `json:"name"`
	Content  string `json:"content"`
	Proxied  bool   `json:"proxied"`
	TTL      int    `json:"ttl"`
	Priority int    `json:"priority"`
}

type response struct {
	Success bool      `json:"success"`
	Errors  []cfError `json:"errors"`
}

type responseWithZones struct {
	response
	Result []Zone `json:"result"`
}

type responseWithRecords struct {
	response
	Result []DNSRecord `json:"result"`
}

type responseWithRecord struct {
	response
	Result DNSRecord `json:"result"`
}

type cfError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func NewClient(token string) *Client {
	return &Client{
		Token: token,
		HTTP:  &http.Client{Timeout: 15 * time.Second},
	}
}

func ExtractBaseDomain(fqdn string) (string, error) {
	parts := strings.Split(strings.TrimSpace(fqdn), ".")
	if len(parts) < 3 {
		return "", fmt.Errorf("invalid domain: %s (need at least sub.example.com)", fqdn)
	}
	return strings.Join(parts[len(parts)-2:], "."), nil
}

func (c *Client) FindZoneByName(zoneName string) (*Zone, error) {
	req, _ := http.NewRequest("GET", baseURL+"/zones?name="+url.QueryEscape(zoneName), nil)
	req.Header.Set("Authorization", "Bearer "+c.Token)

	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, fmt.Errorf("CF API request failed: %w", err)
	}
	defer resp.Body.Close()

	var data responseWithZones
	if err := decodeBody(resp.Body, &data); err != nil {
		return nil, err
	}
	if !data.Success {
		return nil, fmt.Errorf("CF API error: %s", firstError(data.Errors))
	}
	if len(data.Result) == 0 {
		return nil, fmt.Errorf("zone not found: %s", zoneName)
	}
	return &data.Result[0], nil
}

func (c *Client) FindDNSRecord(zoneID, recordType, fqdn, target string) (*DNSRecord, error) {
	req, _ := http.NewRequest("GET", baseURL+"/zones/"+zoneID+"/dns_records?type="+url.QueryEscape(recordType)+"&name="+url.QueryEscape(fqdn), nil)
	req.Header.Set("Authorization", "Bearer "+c.Token)

	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, fmt.Errorf("CF API request failed: %w", err)
	}
	defer resp.Body.Close()

	var data responseWithRecords
	if err := decodeBody(resp.Body, &data); err != nil {
		return nil, err
	}
	if !data.Success {
		return nil, fmt.Errorf("CF API error: %s", firstError(data.Errors))
	}
	for _, record := range data.Result {
		if target == "" || record.Content == target {
			return &record, nil
		}
	}
	return nil, nil
}

func (c *Client) FindMXRecord(zoneID, subdomain, zoneName, target string) (*DNSRecord, error) {
	fqdn := subdomain
	if fqdn == "" {
		fqdn = zoneName
	} else {
		fqdn = subdomain + "." + zoneName
	}
	return c.FindDNSRecord(zoneID, "MX", fqdn, target)
}

func (c *Client) CreateMXRecord(zoneID, subdomain, target string) (*DNSRecord, error) {
	record := DNSRecord{
		Type:     "MX",
		Name:     subdomain,
		Content:  target,
		Priority: 10,
		Proxied:  false,
		TTL:      1,
	}
	payload, _ := json.Marshal(record)

	req, _ := http.NewRequest("POST", baseURL+"/zones/"+zoneID+"/dns_records", strings.NewReader(string(payload)))
	req.Header.Set("Authorization", "Bearer "+c.Token)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, fmt.Errorf("CF API request failed: %w", err)
	}
	defer resp.Body.Close()

	var data responseWithRecord
	if err := decodeBody(resp.Body, &data); err != nil {
		return nil, err
	}
	if !data.Success {
		return nil, fmt.Errorf("CF API error: %s", firstError(data.Errors))
	}
	return &data.Result, nil
}


func (c *Client) CreateTXTRecord(zoneID, name, content string) (*DNSRecord, error) {
	txtContent := content
	if !strings.HasPrefix(txtContent, "\"") {
		txtContent = "\"" + txtContent + "\""
	}
	record := DNSRecord{
		Type:    "TXT",
		Name:    name,
		Content: txtContent,
		Proxied: false,
		TTL:     1,
	}
	payload, _ := json.Marshal(record)

	req, _ := http.NewRequest("POST", baseURL+"/zones/"+zoneID+"/dns_records", strings.NewReader(string(payload)))
	req.Header.Set("Authorization", "Bearer "+c.Token)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, fmt.Errorf("CF API request failed: %w", err)
	}
	defer resp.Body.Close()

	var data responseWithRecord
	if err := decodeBody(resp.Body, &data); err != nil {
		return nil, err
	}
	if !data.Success {
		return nil, fmt.Errorf("CF API error: %s", firstError(data.Errors))
	}
	return &data.Result, nil
}

func (c *Client) DeleteDNSRecord(zoneID, recordID string) error {
	req, _ := http.NewRequest("DELETE", baseURL+"/zones/"+zoneID+"/dns_records/"+recordID, nil)
	req.Header.Set("Authorization", "Bearer "+c.Token)

	resp, err := c.HTTP.Do(req)
	if err != nil {
		return fmt.Errorf("CF API request failed: %w", err)
	}
	defer resp.Body.Close()

	var data response
	if err := decodeBody(resp.Body, &data); err != nil {
		return err
	}
	if !data.Success {
		return fmt.Errorf("CF API error: %s", firstError(data.Errors))
	}
	return nil
}

func decodeBody(r io.Reader, out any) error {
	body, err := io.ReadAll(r)
	if err != nil {
		return fmt.Errorf("CF API read error: %w", err)
	}
	if err := json.Unmarshal(body, out); err != nil {
		return fmt.Errorf("CF API parse error: %w", err)
	}
	return nil
}

func firstError(errors []cfError) string {
	if len(errors) == 0 {
		return "unknown error"
	}
	return errors[0].Message
}
