package auth

import (
	"fmt"
	"net/smtp"
	"strings"
)

// EmailConfig holds Gmail SMTP credentials.
type EmailConfig struct {
	User     string // Gmail address, e.g. you@gmail.com
	Password string // Gmail app password (16-char, no spaces)
	BaseURL  string // e.g. https://the-van-is-secure.fly.dev
}

// Enabled reports whether email sending is configured.
func (cfg EmailConfig) Enabled() bool {
	return cfg.User != "" && cfg.Password != ""
}

// SendMagicLink sends a sign-in email with the one-time magic link.
func (cfg EmailConfig) SendMagicLink(to, token string) error {
	if !cfg.Enabled() {
		return fmt.Errorf("email not configured (GMAIL_USER / GMAIL_APP_PASSWORD not set)")
	}

	link := cfg.BaseURL + "/api/auth/verify?token=" + token

	body := strings.Join([]string{
		"From: The Van Is Secure <" + cfg.User + ">",
		"To: " + to,
		"Subject: Your sign-in link — The Van Is Secure",
		"MIME-Version: 1.0",
		"Content-Type: text/html; charset=UTF-8",
		"",
		`<!DOCTYPE html>
<html>
<head><meta charset="UTF-8"></head>
<body style="background:#0d0d0d;color:#c0c0c0;font-family:monospace;padding:40px;max-width:520px;margin:0 auto">
  <div style="color:#666;letter-spacing:3px;font-size:11px;margin-bottom:8px">▓▒░ CLASSIFIED — ECONOMIC INTELLIGENCE ░▒▓</div>
  <h1 style="font-family:Georgia,serif;color:#ff1030;margin:0 0 24px;font-size:28px">THE VAN IS SECURE</h1>
  <p style="color:#e0e0e0;line-height:1.6">Click below to sign in. This link expires in <strong>15 minutes</strong> and can only be used once.</p>
  <a href="` + link + `" style="display:inline-block;margin:24px 0;padding:14px 28px;background:#ff1030;color:#000;text-decoration:none;font-weight:bold;font-family:monospace;letter-spacing:2px;font-size:13px">▶ SIGN IN NOW</a>
  <p style="color:#444;font-size:11px;margin-top:32px;border-top:1px solid #222;padding-top:16px">
    If you didn't request this link, ignore this email. No account will be created.<br>
    Link: ` + link + `
  </p>
</body>
</html>`,
	}, "\r\n")

	auth := smtp.PlainAuth("", cfg.User, cfg.Password, "smtp.gmail.com")
	return smtp.SendMail("smtp.gmail.com:587", auth, cfg.User, []string{to}, []byte(body))
}

// SendAlertFired sends an alert notification email.
func (cfg EmailConfig) SendAlertFired(to, alertName string, conditions []string) error {
	if !cfg.Enabled() {
		return fmt.Errorf("email not configured")
	}

	condList := ""
	for _, c := range conditions {
		condList += "  <li style='margin-bottom:6px'>" + c + "</li>"
	}

	body := strings.Join([]string{
		"From: The Van Is Secure <" + cfg.User + ">",
		"To: " + to,
		"Subject: ⚠ ALERT FIRED: " + alertName + " — The Van Is Secure",
		"MIME-Version: 1.0",
		"Content-Type: text/html; charset=UTF-8",
		"",
		`<!DOCTYPE html>
<html>
<head><meta charset="UTF-8"></head>
<body style="background:#0d0d0d;color:#c0c0c0;font-family:monospace;padding:40px;max-width:520px;margin:0 auto">
  <div style="color:#666;letter-spacing:3px;font-size:11px;margin-bottom:8px">▓▒░ ALERT NOTIFICATION ░▒▓</div>
  <h1 style="font-family:Georgia,serif;color:#ff1030;margin:0 0 8px;font-size:24px">THE VAN IS SECURE</h1>
  <h2 style="color:#ffaa00;margin:0 0 24px;font-size:16px">⚠ ` + alertName + `</h2>
  <p style="color:#e0e0e0">The following conditions are all currently true:</p>
  <ul style="color:#00e87a;line-height:1.8">` + condList + `</ul>
  <a href="` + cfg.BaseURL + `" style="display:inline-block;margin:24px 0;padding:12px 24px;background:#ff1030;color:#000;text-decoration:none;font-weight:bold;font-family:monospace;letter-spacing:2px;font-size:12px">▶ VIEW DASHBOARD</a>
  <p style="color:#444;font-size:11px;margin-top:32px;border-top:1px solid #222;padding-top:16px">
    Manage your alerts at ` + cfg.BaseURL + `
  </p>
</body>
</html>`,
	}, "\r\n")

	auth := smtp.PlainAuth("", cfg.User, cfg.Password, "smtp.gmail.com")
	return smtp.SendMail("smtp.gmail.com:587", auth, cfg.User, []string{to}, []byte(body))
}
