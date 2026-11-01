package mail

import (
	"bytes"
	"fmt"
	"html/template"
)

var layout = template.Must(template.New("l").Parse(`<!doctype html><html><body style="margin:0;background:#0D0D0E;font-family:Arial,Helvetica,sans-serif;color:#FFFFFF">
<table width="100%" cellpadding="0" cellspacing="0" role="presentation"><tr><td align="center" style="padding:32px 16px">
<table width="480" cellpadding="0" cellspacing="0" role="presentation" style="max-width:480px;background:#161618;border:1px solid #27272A;border-radius:12px">
<tr><td style="padding:28px 28px 8px;font-size:20px;font-weight:700;letter-spacing:.5px"><span style="color:#FF003C">RED</span>XTUBE</td></tr>
<tr><td style="padding:8px 28px 4px;font-size:16px;font-weight:600">{{.Title}}</td></tr>
<tr><td style="padding:4px 28px 16px;font-size:14px;line-height:1.6;color:#A1A1AA">{{.Intro}}</td></tr>
{{if .Code}}<tr><td align="center" style="padding:8px 28px 16px"><div style="display:inline-block;padding:14px 22px;background:#222225;border:1px solid #27272A;border-radius:10px;font-size:32px;font-weight:700;letter-spacing:8px;font-family:Consolas,monospace">{{.Code}}</div></td></tr>{{end}}
{{if .Link}}<tr><td align="center" style="padding:8px 28px 16px"><a href="{{.Link}}" style="display:inline-block;padding:12px 22px;background:#FF003C;color:#FFFFFF;text-decoration:none;border-radius:8px;font-weight:600;font-size:14px">{{.Button}}</a></td></tr>{{end}}
<tr><td style="padding:0 28px 28px;font-size:12px;line-height:1.6;color:#71717A">{{.Footer}}</td></tr>
</table></td></tr></table></body></html>`))

type view struct {
	Title, Intro, Code, Link, Button, Footer string
}

func render(to, subject string, v view, text string) Message {
	var b bytes.Buffer
	if err := layout.Execute(&b, v); err != nil { // html/template escapes every field
		return Message{To: to, Subject: subject, Text: text}
	}
	return Message{To: to, Subject: subject, Text: text, HTML: b.String()}
}

// VerifyCode is the 6-digit email verification message.
func VerifyCode(to, code string, ttlMinutes int) Message {
	text := fmt.Sprintf("Your REDXTUBE verification code is %s.\n\nIt expires in %d minutes. If you didn't create a REDXTUBE account, you can ignore this email.", code, ttlMinutes)
	return render(to, "Your REDXTUBE verification code", view{
		Title:  "Verify your email",
		Intro:  fmt.Sprintf("Enter this code to finish creating your account. It expires in %d minutes.", ttlMinutes),
		Code:   code,
		Footer: "If you didn't create a REDXTUBE account, you can safely ignore this email. Never share this code with anyone.",
	}, text)
}

// ResetPassword carries the single-use reset link.
func ResetPassword(to, link string, ttlMinutes int) Message {
	text := fmt.Sprintf("Reset your REDXTUBE password:\n%s\n\nThe link expires in %d minutes and works once. If you didn't ask for this, ignore this email; your password has not changed.", link, ttlMinutes)
	return render(to, "Reset your REDXTUBE password", view{
		Title:  "Reset your password",
		Intro:  fmt.Sprintf("Use the button below to choose a new password. The link expires in %d minutes and can be used once.", ttlMinutes),
		Link:   link,
		Button: "Choose a new password",
		Footer: "If you didn't ask for this, you can ignore this email; your password has not changed.",
	}, text)
}

// PasswordChanged is the security notice sent after a password change or reset.
func PasswordChanged(to string) Message {
	text := "Your REDXTUBE password was just changed and you were signed out of other devices.\n\nIf this wasn't you, reset your password immediately and contact support."
	return render(to, "Your REDXTUBE password was changed", view{
		Title:  "Password changed",
		Intro:  "Your password was just changed and other devices were signed out.",
		Footer: "If this wasn't you, reset your password right away and contact support.",
	}, text)
}
