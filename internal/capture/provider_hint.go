package capture

import "net/http"

func ProviderHint(headers http.Header, path string) string {
	switch {
	case headers.Get("Stripe-Signature") != "":
		return "stripe"
	case headers.Get("X-GitHub-Event") != "" || headers.Get("X-Hub-Signature-256") != "":
		return "github"
	case headers.Get("X-Paystack-Signature") != "":
		return "paystack"
	case headers.Get("Verif-Hash") != "":
		return "flutterwave"
	case headers.Get("X-Shopify-Hmac-Sha256") != "":
		return "shopify"
	case headers.Get("X-Telegram-Bot-Api-Secret-Token") != "":
		return "telegram"
	case headers.Get("X-Signature-Ed25519") != "" || headers.Get("X-Signature-Timestamp") != "":
		return "discord"
	case path == "/hooks/telegram":
		return "telegram"
	default:
		return "unknown"
	}
}
