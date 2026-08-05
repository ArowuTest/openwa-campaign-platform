package dispatch

import "testing"

func TestGatewayMessageTypeMapping(t *testing.T) {
	cases := map[string]string{"TEXT": "text", "IMAGE_CAPTION": "image", "VIDEO": "video", "DOCUMENT": "document"}
	for input, expected := range cases {
		got, err := gatewayMessageType(input)
		if err != nil || got != expected {
			t.Fatalf("%s => %s %v", input, got, err)
		}
	}
	if _, err := gatewayMessageType("VOICE"); err == nil {
		t.Fatal("unsupported type accepted")
	}
}
