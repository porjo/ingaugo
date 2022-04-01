package ingaugo

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"image"
	"image/png"
	"strconv"

	"github.com/chromedp/cdproto/cdp"
	"github.com/chromedp/cdproto/network"
	dp "github.com/chromedp/chromedp"
	"github.com/vitali-fedulov/images3"
)

// Login takes a context, ING client number and access pin and returns an authentication token
func (bank *Bank) Login(ctx context.Context, clientNumber, accessPin string) (token string, err error) {
	if clientNumber == "" {
		return "", fmt.Errorf("clientNumber is required")
	}
	if accessPin == "" {
		return "", fmt.Errorf("accessPin is required")
	}

	var cancel context.CancelFunc
	if bank.wsURL != "" {
		ctx, cancel = dp.NewRemoteAllocator(ctx, bank.wsURL)
		defer cancel()
	}
	ctx, cancel = dp.NewContext(ctx)
	defer cancel()

	var imgNodes []*cdp.Node

	tokenResponseChan := make(chan *network.EventResponseReceived)
	clickTasks := make(dp.Tasks, 0)

	dp.ListenTarget(ctx, func(ev interface{}) {
		switch ev := ev.(type) {
		case *network.EventResponseReceived:
			if ev.Response.URL == "https://www.ing.com.au/STSServiceB2C/V1/SecurityTokenServiceProxy.svc/issue" {
				tokenResponseChan <- ev
			}
		}
	})

	clog.Printf("Fetching page: %s\n", loginURL)
	if err := dp.Run(ctx,
		dp.Navigate(loginURL),
		dp.WaitVisible("#loginInput", dp.ByID),
		dp.SendKeys("#cifField", clientNumber, dp.ByID),
		dp.Nodes(".pin > img", &imgNodes, dp.ByQueryAll),
		dp.ActionFunc(func(ctx context.Context) error {
			var err error
			clog.Println("Generating pin clicks")
			clickTasks, err = bank.generatePinClicks(ctx, accessPin, imgNodes)
			if err != nil {
				return err
			}
			return nil
		}),
	); err != nil {
		return "", fmt.Errorf("Chrome actions failed: %v", err)
	}

	// clickTasks needs to be handled in separate Run() clause, why?
	if err := dp.Run(ctx,
		clickTasks,
		dp.ActionFunc(func(ctx context.Context) error {
			clog.Println("Performing login")
			return nil
		}),
		dp.Click("#login-btn", dp.ByID),
	); err != nil {
		return "", fmt.Errorf("Chrome actions failed: %v", err)
	}

	clog.Printf("Wait for token response\n")
	ev := <-tokenResponseChan
	if err := dp.Run(ctx,
		dp.ActionFunc(func(ctx context.Context) error {
			body, err := network.GetResponseBody(ev.RequestID).Do(ctx)
			if err != nil {
				return err
			}
			tr := tokenResponse{}
			err = json.Unmarshal(body, &tr)
			if err != nil {
				return err
			}
			token = tr.Token
			return nil
		}),
	); err != nil {
		return "", fmt.Errorf("Chrome actions failed: %v", err)
	}

	return
}

func (bank *Bank) generatePinClicks(ctx context.Context, accessPin string, imgNodes []*cdp.Node) (dp.Tasks, error) {
	clickTasks := make(dp.Tasks, 0)
	randomKeys := make([]string, 0)
	for _, node := range imgNodes {
		src, ok := node.Attribute("src")
		if !ok {
			continue
		}
		randomKeys = append(randomKeys, src[22:])
	}
	keymap, err := generateKeymap(randomKeys)
	if err != nil {
		return nil, err
	}
	for _, r := range accessPin {
		digit, _ := strconv.Atoi(string(r))
		clickIdx, ok := keymap[digit]
		if ok {
			clickTasks = append(clickTasks, dp.Click(".uia-pin-"+strconv.Itoa(clickIdx), dp.ByQuery))
		}
	}
	return clickTasks, nil
}

func generateKeymap(randomKeys []string) (map[int]int, error) {
	keypadMap := make(map[int]int)
	keypadImages, err := getKeypadImages()
	if err != nil {
		return nil, err
	}
	for randIdx, b := range randomKeys {
		unbased, err := base64.StdEncoding.DecodeString(b)
		if err != nil {
			return nil, err
		}

		r := bytes.NewReader(unbased)
		randImg, err := png.Decode(r)
		if err != nil {
			return nil, err
		}
		for keyIdx, keyImg := range keypadImages {

			icon1 := images3.Icon(randImg, "")
			icon2 := images3.Icon(keyImg, "")

			m1, m2, m3 := images3.EucMetric(icon1, icon2)
			if m1 < 20.0 && m2 < 20.0 && m3 < 20.0 {
				keypadMap[keyIdx] = randIdx
				break
			}
		}
	}
	return keypadMap, nil
}

func getKeypadImages() ([]image.Image, error) {
	b64 := []string{
		`iVBORw0KGgoAAAANSUhEUgAAALQAAABuCAYAAACOaDl7AAAAAXNSR0IArs4c6QAAAARnQU1BAACxjwv8YQUAAAAJcEhZcwAADsMAAA7DAcdvqGQAAAPfSURBVHhe7dyxShxRGIbh4xZ6Efa5kNyDkGpFkDRB06QNeAFJnyJdukDqpE0RLCxUSCXYBhSJhUK0cTLfOBNk8s/uOntWZj/eHx5017NbvRzOzu6amik20ur55mj3fDzaL39elQpgwK7qVnfVbp3x/VyM03r5x8PWA4DlULarhquYq525jvlya1TcbK8Udy9TUQADpkbVqpqtoz462Ulr6WxztNPETMhYNmq2iVotp/ocUtUePQAYOrVb79L72qFvdYPdGctK7dZB36Tql1K0EFgWTccEDQsEDSsEDSsEDSsEDSsEDSsEDSsEDSsEDSsEDSsEDSsEDSsEDSsEDSsEDSsEDSsEDSsEDSsEDSsEDSsEDSsEDSsEDSsEDSsEDSsEDSsEDSsEDSsEDSsEDSsEDSsE/ZTevSiKD6/u7T2P12AuBL1oivfnjyKcP9dFcfCNuDMi6EV586w75PYo7C/v4+fBoxD0IijmX6d1rVNmr1zfDFHPjaAXQceIvqMjSvScmAlB56Ygu0ZHkK8fi+L756L4fVbf2RrdHz0vZkLQuZ0e12W2pr3zTjqWcPTojaBz0mW5aLQrR+sVtV4Qtkc7ebQeUxF0TjpKRKNwo/XSdd6O1mIqgs4pOkJM220/va0XtoYXh70QdE7RdB03Gtq9o5n2OIQIOpeuqxuz7LTREHQvBJ1LV9B6oRitfyg6quhqSbQWExF0LtpRo4nWtkWX+gi6F4LOhaAHgaBzIehBIOhcCHoQCDoXgh4Egs5lnqCjDyrpXcdoLSYi6Fy4Dj0IBJ1L36C7PtCkt8Sj9ZiIoHOKZtpOq4+KRsP3DHsh6Jyid/x0X7S2EX3vkA/590bQOXV9fLRrt9X90fCCsDeCzqnrPKxdOvpMdHS5TsNxozeCzq0rUkWt87JeJOpndDzR6AP/0fNiJgSdW9cuPcvo61jsznMh6EXounIxbbhUNzeCXpTHRM1/TsqGoBdJ5+WuM3Uz/G+7rAj6KShYHSf0JktDsU/6Njh6IWhYIWhYIWhYIWhYIWhYIWhYIWhYIWhYIWhYIWhYIWhYIWhYIWhYIWhYIWhYIWhYIWhYIWhYIWhYIWhYIWhYIWhYIWhYIWhYIWhYIWhYIWhYIWhYIWhYIWhYIWhYIWhYIWhYIWhYIWhY+Rf02eboVr/cBYuAZaB266Cv0vl4tK8bN9sr4WJg6NRuFXTZso4cu7pxucUujeWjZtVuFXTZcjrZSWtl2UdN1KqdsDF0alStPoj5WC0nzcU4rZdRH9Z/AJZLuSGr4SrmZoqNtFr+4XX5IvGgXHT934OAYbmuWi2bVbv3Faf0F1j7ej7rw44CAAAAAElFTkSuQmCC`,
		`iVBORw0KGgoAAAANSUhEUgAAALQAAABuCAYAAACOaDl7AAAAAXNSR0IArs4c6QAAAARnQU1BAACxjwv8YQUAAAAJcEhZcwAADsMAAA7DAcdvqGQAAAM2SURBVHhe7dyxTttQFIfxiwd4CPY+SN8BqVMiJNSlSrp0rcQDtHuHbt0qdW5XpgwMwIrUB0iEypBIDQu39zh2i8JFgWAf2f9+R/qJ2LE9fbpyIpxQTzwIu7NhMZ4Nikn6O08i0GHzqtWxtVtlvJqrQdhPb56tnQD0Q2rXGi5jLlfmKubrwyIuj3bi7esQI9Bh1qi1as1WUZ9fjsJemA6LUR0zIaNvrNk6ams5VPchZe25E4Cus3arVXpiK/SNbbA6o6+s3SroZShfJLkDgb6oOyZoSCBoSCFoSCFoSCFoSCFoSCFoSCFoSCFoSCFoSCFoSCFoSCFoSCFoSCFoSCFoSCFoSCFoSCFoSCFoSCFoSCFoSCFoSCFoSCFoSCFoSCFoSCFoSCFoSCFoSCFoSCFoSCFoTx9exXjyNcafF/8cv8wfi60QtIdvH1fx5ubTm/w52ApBt8lC/jWtyn1gCLpRBN20dy9i/P55c8j1EHSjCLpJdo/8e1GV+sgh6EYRdJPsA95Th6AbRdBNy334s3327UZuCLpRBN00C7Se0x+r25D1/XeHoBtF0G348v7+98sE7YKgvRC0C4L2QtAuCNoLQbsgaC8E7YKgvRC0C4L2QtAuCNoLQbsgaC8E7YKgvRC0C4L2QtAuCNoLQbsgaC8E7YKgvRC0C4L2QtAuCNoLQbsg6DbYP/VbqHfZg7O5sf3rx9qDtrnrYiOCbsNTH5RdH/v5g9x1sRFBt+G5Y6t27rrYiKDb8Nwh6K0RdBse+yMzD409k5i7LjYiaEghaEghaEghaEghaEghaEghaEghaEghaEghaEghaDXHmX3/EYKGFIKGFIKGFIKGFIKGFIKGFIKGFIKGFIKGFIKGFIKGFIKGFIKGFIKGFIKGFIKGFIKGFIKGFIKGFIKGFIKGFIKGFIKGFIKGFIKGlL9BT4fFjb24zRwE9IG1WwU9D7NBMbGN5dFO9mCg66zdMujUst1yjG3j+pBVGv1jzVq7ZdCp5XA5Cnup7PM6aqudsNF11qi1eifmC2s52FwNwn6K+qx6A+iXtCBbw2XM9cSDsJveeJs+JJ6mgxb3TgK6ZVG2mpq1dlcVh/AHlHnTNopMQR4AAAAASUVORK5CYII=`,
		`iVBORw0KGgoAAAANSUhEUgAAALQAAABuCAYAAACOaDl7AAAAAXNSR0IArs4c6QAAAARnQU1BAACxjwv8YQUAAAAJcEhZcwAADsMAAA7DAcdvqGQAAAOtSURBVHhe7dsxS9xgHMfxxxv0Rbj3hfQ9CJ1OBOlStEvXgi+g3Tt061bo3K4dioODCp0E14IidVCoLj7NL5eUIz6hOZNcLj++f/hQr5fL9PXhSS6GcuJWWL/cnuxfTieH2b83mQissJui1X21W2Q8m6tp2MzePK58ABiHrF01nMecr8xFzNc7k3i3uxYfXoYYgRWmRtWqmi2iPjnbCxvhYnuyV8ZMyBgbNVtGrZZDsQ/Ja099AFh1ardYpQ+1Qt/rBaszxkrtFkHfhfyHTOpAYCzKjgkaFggaVggaVggaVggaVggaVggaVggaVggaVggaVggaVggaVggaVggaVggaVggaVggaVggaVggaVggaVggaVggaVggaVggaVggaVggaVggaVggaVggaVggaVggaVgh6Wd69iPHDqxn9nDoGrRF0X948i/HL+xjPT2Pt/PwxCzz1eTwJQffh68cY/9wW1TaYo2+zX4DUubAQgu6Sovx1XlS64Gi1Tp0TCyHoLh08L+p84miLkjovGiPormmlrY720dqGSOr9cn5fpM+Jxgi6a1qly/2zAtbr6jG6y1G3x+YOSCsE3QfduUiFPE/bi9TolyB1PBoh6KHoAjI1BN0KQQ8pNQTdCkEPhRW6FwQ9lE9vi4Irw0VhKwQ9FH07WB3d+Ugdi8YIegh1X8Cw3WiNoIeQemBJqzPPc7RG0MumVTg1rM6dIOhlqvuGUA80pY53dpD4vw4Q9LJoO6FnNaqjwLmz0RmCXpa6B/15wq5TBL0M3z8X9VZGt+5Sx+PJCLpvdQ8hacVOHY9WCLpP2hunRheB3KLrBUH3Zf656PnhfnOvCLoPCjb1t4Xc0egdQfeh7s+siLl3BN21um8CtTrrQvB/dEckdV40QtBdqrsIXGQUdercaISgu1R3i26RIehWCLpLdduNRYagWyHoLrFCD46gYYWgYYWgYYWgYYWgYYWgYYWgYYWgYYWgYYWgYYWgYYWgYYWgYYWgYYWgYYWgYYWgYYWgYYWgYYWgYYWgYYWgYYWgYYWgYYWgYYWgYYWgYYWgYYWgYYWgYYWgYYWgYYWgYYWgYYWgYYWgYeVf0Bfbk3v98JA4CBgDtVsEfRMup5NDvbjbXUseDKw6tZsHnbWsLce+XlzvsEpjfNSs2s2DzloOZ3thIyv7pIxatRM2Vp0aVatzMZ+q5aC5mobNLOrj4g1gXLIFWQ3nMZcTt8J69sbr7CLxKDvo9tGHgNVym7eaNat2ZxWH8BfxOfqaaAHK8QAAAABJRU5ErkJggg==`,
		`iVBORw0KGgoAAAANSUhEUgAAALQAAABuCAYAAACOaDl7AAAAAXNSR0IArs4c6QAAAARnQU1BAACxjwv8YQUAAAAJcEhZcwAADsMAAA7DAcdvqGQAAAPYSURBVHhe7dyxShxBAMbx8Qp9CPs8SN5BSHUihDRB06QN+ABJnyJdukDqpE0RLCxUSCXYBhSJhUK0cbLfupscl1kd3V339uM/8EPPm93qzzC3u2eoR1wLyyfrk62T6WSn+HleiMACO69a3VK7VcY343QaVos39+YOAMahaFcNlzGXK3MV89nGJF4+X4rXL0KMwAJTo2pVzVZR7x9uhpVwvD7ZrGMmZIyNmq2jVsuh2oeUtacOABad2q1W6R2t0Fd6weqMsVK7VdCXofylkJoIjEXdMUHDAkHDCkHDCkHDCkHDCkHDCkHDCkHDCkHDCkHDCkHDSn7Q24m/AQsmP2hgBAgaVggaVggaVggaVggaVggaVggaVggaVggaVggaVggaVggaVggaVggaVggaVggaVggaVggaVgj6Mbx+EuP7l/9sP03PQ2sE3Ze3z2L89inGX8cxOX5fxLj7lbg7RtBdU6BHB1W1mePzu/S5cG8E3bUvH6pK7zl0XOp8uBeC7ppW6IcObVNS50Q2gu6D9sb1+PH9ZktRfyDUvlr759TQ3NT5kI2g+6CrGtpC6Gfqfa3ETVGn5iMbQQ+laa+tVTw1H1kIeigKNzUIuhWCHgpB94Kgh6IPh/ND++rUXGQj6CE0fShU5Kn5yEbQj0nbiabLdj+Pmq+KIBtB9+njm6rWO4Zi5pmOThB0n3Jug+smTOpYPAhB9yn3uQ5FzXajEwTdJ93yzh3aV/MsR2sE3af5B/u1Yms1brrtrb106jzIRtBDUOiKNzV4NroVgh6Kok6t1PpyQGo+shD0kGYfM50dqbnIQtBDaroKkpqLLAQ9pNTzHBqpuchC0EPRHjr1jXCudLRC0F3Tvlhuu5WtmJv2z/p76hhkIegu6Vrz7NBqq33y/LXopv/VoaE5qXMjC0F3SV9ybTO4ZNcaQXepzdA1aW1FUudFNoLukrYTTbe1bxs8PtoZgu6aVtm79sn1UMjc6u4UQfdJq279QXCW/saK3AuChhWChhWChhWChhWChhWChhWChhWChhWChhWChhWChhWChhWChhWChhWChhWChhWChhWChhWChhWChhWChhWChhWChhWChhWChhWChhWChhWChhWChhWChhWChhWChhWChhWChhWChpW/QR+vT670y3ViEjAGarcK+jycTCc7enH5fCk5GVh0arcMumhZW44tvTjbYJXG+KhZtVsGXbQcDjfDSlH2fh21aidsLDo1qlZnYj5Qy0HjdBpWi6j3qjeAcSkWZDVcxlyPuBaWizdeFR8Sd4tJF/8dBCyWi7LVolm1e1NxCH8AkL8GhK3kcVwAAAAASUVORK5CYII=`,
		`iVBORw0KGgoAAAANSUhEUgAAALQAAABuCAYAAACOaDl7AAAAAXNSR0IArs4c6QAAAARnQU1BAACxjwv8YQUAAAAJcEhZcwAADsMAAA7DAcdvqGQAAAN7SURBVHhe7dsxTxRBGIfx4Qr4EPR+EL8DidUREmJjOBtbEz6A9hZ2dibW2loYCgogsSKhNYEQKSARGtZ5l1lzLC8Gb2bZ3b/Pm/wix83F5nGys7eGZqq1sHyyPpmdTCc78c/zqAIG7Dy1OrN2U8Y3czoNq/HNvdYHgHGI7VrDdcz1zpxiPtuYVJebS9X181BVwIBZo9aqNZui3j/cCivheH2y1cRMyBgba7aJ2loO6Tqkrt37ADB01m7apXdsh76yF+zOGCtrNwV9GeofIm8hMBZNxwQNCQQNKQQNKQQNKQQNKQQNKQQNKQQNKQQNKQQNKQQNKQQNKQQNKQQNKQQNKQQNKQQNKQQNKQQNKQQNKQQNKQQNKQQNKQQNKQQNKQQNKQQNKQQNKQQNKQQNKQQNKQQNKQT9WHa/VNXRwW3vXvhrsTCCfgwfXlfubD/112NhBP0Yvn9LBc+N7dDeWmQh6K7ZLuzNp7f+emQh6K59fp8KnptfF/5aZCPorlm87bHIvbXIRtBdsssKbzgMdoagu2QHv/bYAdFbiyIIuiv3HQa599wpgu7K14+p4Ln5eeyvRTEE3RUOg70g6C7cdxh89cRfj2IIugveYdCe5ZhfY1+H247dsH8E3P3IRtClvXmWCm6N/X5+nTdckmQj6NJsJ27Pj6O767wh6GwEXZJdI3uHQe+5DW8IOhtBl+QdBi1w7zDoDUFnI+iS7NKiPXY/2lvrDUFnI+hS7jsMLjo8kbcQgi7FdtfS4/09+CuCLoWgB4GgSyHoQSDoUuybv+Z/cz+EN/bwUvN++5tFPAhB98Ub7nJkI+i+eEPQ2Qi6L978D0FvO78riKD74g07dDaC7os3BJ2NoPtidzTa4z3EhH9C0JBC0JBC0JBC0JBC0JBC0JBC0JBC0JBC0JBC0JBC0JBC0JBC0JBC0JBC0JBC0JBC0JBC0JBC0JBC0JBC0JBC0JBC0JBC0JBC0JBC0JBC0JBC0JBC0JBC0JBC0JBC0JBC0JBC0JBC0JBC0JDyJ+jj9cmV/XDtLALGwNpNQZ+Hk+lkx15cbi65i4Ghs3broGPLdskxsxdnG+zSGB9r1tqtg44th8OtsBLL3m+ittoJG0NnjVqrczEfWMvB5nQaVmPUe+kNYFzihmwN1zE3U62F5fjGy3hI3I2LLu58CBiWi7rV2Ky1e1NxCL8BJDbwRJUAIFgAAAAASUVORK5CYII=`,
		`iVBORw0KGgoAAAANSUhEUgAAALQAAABuCAYAAACOaDl7AAAAAXNSR0IArs4c6QAAAARnQU1BAACxjwv8YQUAAAAJcEhZcwAADsMAAA7DAcdvqGQAAAOGSURBVHhe7dqxTttQGIbhQwa4CPZeSO8BqVMQEupSQZeulbiAdu/QrVulzu3aiYEBWJFYK4FQGUAqLLjnc+wqoifgOD44/nh/6VFjYnt6e+TYDvUUG2H1fHO0ez4e7cd/r6ICWGJXVau7arfKeDIX47Aevzy8dwAwDLFdNVzGXK7MVcyXW6PiZnuluHsdigJYYmpUrarZKuqjk52wFs42Rzt1zISMoVGzddRqOVTXIWXtqQOAZad2q1V6Xyv0rTZYnTFUarcK+iaUH6LUjsBQ1B0TNCwQNKwQNKwQNKwQNKwQNKwQNKwQNKwQNKwQNKwQNKwQNKwQNKwQNKwQNKwQNKwQNKwQNKwQNKwQNKwQNKwQNKwQNKwQNKwQNKwQNKwQNKwQNKwQNKwQNKwQNKwQNKwQdA7fPxfF6XE7716kz4lGCDoHhdl2Pr1JnxONEHQOBN0bgs6BoHtD0DksEvTey/Q50QhB55AKmpX3SRB0DgTdG4LOgaB7Q9A5EHRvCDoHgu4NQeeQCvrgx+QJYo3AsyDoHFJBzxqFzq26zhB0DvMErflzXRTfPqbPhbkQdA7zBl3Pl/fp86Exgs5hOuhfp5Nt0eeHRis1b9sthKBz0DXxrOtiBasfhYo3NfoudRwaed5B7yX+9lQ+vKoKvje/z9L7o5HnHXTfdIcjNVx2tEbQfdKdjdRwj7o1gu6Twk0NQbdG0H0i6M4RdJ90RyM1XEO3RtB9UbS6o3F/uMuxEILu2s+vk4coD72foZhn3eHQ8alj0AhBd0kRT4+eDNZv1tW0nVqZ6+FFpYUQdJdmrbpNh6eECyPoLj208j42+s+QOifmQtBd0iVFm6i5bu4MQeegJ4CPvVmn0aqsdzpS50ArBJ2TfuDVPwSn6W+p/bEwgoYVgoYVgoYVgoYVgoYVgoYVgoYVgoYVgoYVgoYVgoYVgoYVgoYVgoYVgoYVgoYVgoYVgoYVgoYVgoYVgoYVgoYVgoYVgoYVgoYVgoYVgoYVgoYVgoYVgoYVgoYVgoYVgoYVgoYVgoaVf0GfbY5u9eEusRMwBGq3CvoqnI9H+9q42V5J7gwsO7VbBh1b1iXHrjYut1ilMTxqVu2WQceWw8lOWItlH9VRq3bCxrJTo2p1KuZjtRw0F+OwHqM+rL4AhiUuyGq4jLmeYiOsxi/exh+JB3Gn6/8OApbLddlqbFbtTioO4S8IlxqiUxZsrQAAAABJRU5ErkJggg==`,
		`iVBORw0KGgoAAAANSUhEUgAAALQAAABuCAYAAACOaDl7AAAAAXNSR0IArs4c6QAAAARnQU1BAACxjwv8YQUAAAAJcEhZcwAADsMAAA7DAcdvqGQAAAPzSURBVHhe7duxThRdHIbxwxZwEfReiPdAYrWEhNgYsLE14QK0t7CzM7HW1sJQUACJFQmtCYRIAYnQMM67zOhm/c+yO3NGd1+fk/zyLbtnt3q+kzNnxlSPYiOtnm8Ods+Hg/3yv1elAlhgV1Wru2q3yvh+XAzTevnh4cQXgOVQtquGRzGPVuYq5sutQXGzvVLcPU1FASwwNapW1WwV9dHJTlpLZ5uDnTpmQsayUbN11Go5VfuQUe3RF4BFp3arVXpfK/St/mB1xrJSu1XQN2n0ohRNBJZF3TFBwwJBwwpBwwpBwwpBwwpBwwpBwwpBwwpBwwpBwwpBwwpBwwpBwwpBwwpBwwpBwwpBwwpBwwpBwwpBwwpBwwpBw4pH0HvBe/gveQQNVAgaVggaVggaVggaVggaVggaVggaVgj6X3jz7LcXj+I5aIWg/5YPr4vi22kRju9nRfHxLXFnQNB9e/WkOeTJ8eP6ftWOfgczIeg+KWZFOs/QSh39FmZC0H1pE7Pms+3ohKD70rTN0PtaheuLQr2u57I6d0bQfdAFYDQOPsXzRSt69D7mQtB90KnF5NAqHM1FVgSd27uXVcETQ+9H85EVQeembcXk0MVeNBfZEXRu0cnGtL0zsiLonHRhF43x04v6ZKPGjZSsCDqnaftnxdt0Lq339Tln0J0RdE6KMhrRqUc0dBJC1J0QdE6f31dldhgc73VC0DmdHldVdhxa6aPfx4MIOqemoOvHQ8fvBur11y/VhImh+eO/i5kRdE5R0A9tIZr+J+BWeCsEnVMUp96L5taaTkb0PEg0H1MRdE5tgpZosI9uhaBzik45ZtkPR4OgWyHonJrOoaO546JB0K0QdE66jR2NaU/aNX2HW+KtEHROussXjWkPJ0VP52lwx7AVgs5tnmO4poeZeDqvNYLOremfX+kBpPooTquvXjc9rMR2ozWC7kPTKj3L0ElJ9JuYCUH3QVuJptV32uDBpM4Iui/zRq19MxeCnRF0nxSoQp0WtlZl9szZEPTfomh1s6Smi8K9x/FctEbQsELQsELQsELQsELQsELQsELQsELQsELQsELQsELQsELQsELQsELQsELQsELQsELQsELQsELQsELQsELQsELQsELQsELQsELQsELQsELQsELQsELQsELQsELQsELQsELQsELQsELQsPIr6LPNwa1e3AWTgGWgdqugr9L5cLCvP262V8LJwKJTu6Ogy5a15djVH5dbrNJYPmpW7Y6CLltOJztprSz7qI5atRM2Fp0aVatjMR+r5aRxMUzrZdSH1QfAcikXZDU8irkexUZaLT94Xl4kHpSTrv/4ErBYrketls2q3fuKU/oJJ2Zlsu5hju4AAAAASUVORK5CYII=`,
		`iVBORw0KGgoAAAANSUhEUgAAALQAAABuCAYAAACOaDl7AAAAAXNSR0IArs4c6QAAAARnQU1BAACxjwv8YQUAAAAJcEhZcwAADsMAAA7DAcdvqGQAAANzSURBVHhe7dsxS1tRGIfxYwb9EO79IP0OQqcEQbqUpEvXgh+g3Tt061bo3K4dioODCp0E10JE6qDQuHh73utJCekbNeace3P/PC/80JgTXR4ON7nHMJ1qJ2yeD3qj837vIH69iipgjV2lVkfWbsr4bi76YTs+eTT3AqAbYrvWcB1zvTOnmC93e9Vkb6O6fRmqClhj1qi1as2mqI9Ph2ErjAe94TRmQkbXWLPTqK3lkK5D6tq9FwDrztpNu/SB7dA39oDdGV1l7aagJ6H+JvIWAl0x7ZigIYGgIYWgIYWgIYWgIYWgIYWgIYWgIYWgIYWgIYWgIYWgIYWgIYWgIYWgIYWgIYWgIYWgIYWgIYWgIYWgIYWgIYWgIYWgIYWgIYWgIYWgIYWgIYWgIYWgIYWgIYWgIYWgc9p/XlVnJ6v7+aOq3jzz/wbuRdA5fXhVZRv7Xd7fwL0IOieCbh1B55QzaC45noSgc3r3ItW44vw6838/HkTQbfs9ThXPzJf3/lo8iKDb9OltKnhm/lxzubECgm6TfUQ3P4ff/LV4FIJuy6Lrbfu5tx6PQtBtsZ14fmzH9tbi0Qi6DXaN7A1vBldG0G34+jEVPDP2ZtBbi6UQdBss3vmxyL21WApBN80uK7yxg03eeiyFoJvm3Uix03XeWiyNoJu06KwHB5GyIegmeTdSbMf21uJJCLopdo3sDW8GsyLopng3Uji3kR1BN8Gi9T6q49xGdgTdBO9Gig3nNrIj6CZ4H9VxiL8Igi5t0Y0Uzm0UQdCl2U48P5zbKIagS1p0I+X7Z389VkbQJdktbW84t1EMQZey6EYK5zaKIuhS7LLCG/vHWG89siDoUryP6vgXq+IIGlIIGlIIGlIIGlIIGlIIGlIIGlIIGlIIGlIIGlIIGvntOz9rCEFDCkFDCkFDCkFDCkFDCkFDCkFDCkFDCkFDCkFDCkFDCkFDCkFDCkFDCkFDCkFDCkFDCkFDCkFDCkFDCkFDCkFDCkFDCkFDCkFDyr+gx4PejX1z6ywCusDaTUFfhfN+78AeTPY23MXAurN266Bjy3bJMbIHl7vs0ugea9barYOOLYfTYdiKZR9Po7baCRvrzhq1VmdiPrGWg81FP2zHqI/SE0C3xA3ZGq5jnk61EzbjE6/jm8TDuOj6vxcB6+W6bjU2a+3eVRzCX7pqtoTp9CtwAAAAAElFTkSuQmCC`,
		`iVBORw0KGgoAAAANSUhEUgAAALQAAABuCAYAAACOaDl7AAAAAXNSR0IArs4c6QAAAARnQU1BAACxjwv8YQUAAAAJcEhZcwAADsMAAA7DAcdvqGQAAAQNSURBVHhe7duxThRRGIbhYQu4CHovxHsgsVpCYmwM2NiacAHaW9jZmVhra2EoKIDEioTWBEKkgERoGOdbZnSz/IednT2Du1/eP3kisLNTvTk5e2Ytmik3itWzzcHO2XCwV/17WSmBBXZZt7qjduuM7+Z8WKxXLx5MvAFYDlW7angU82hlrmO+2BqU189XytsXRVkCC0yNqlU1W0d9eLxdrBWnm4PtJmZCxrJRs03Uarmo9yGj2qM3AItO7dar9J5W6Bv9wuqMZaV266Cvi9EPlehCYFk0HRM0LBA0rBA0rBA0rBA0rBA0rBA0rBA0rBA0rBA0rBA0rBA0rBA0rBA0rBA0rBA0rBA0rBA0rBA0rBA0rBA0rBA0rBA0rBA0rBA0rBA0rBA0rBA0rBA0rBA0rBA0rBD0Y9p9WpbvX/4TXYO5EHTfFPH+17L8dVqG8/OkLL98KMvXT+L3YyYE3SeF2nYU/Ntn8X3QGkH3RavyrPP7iqjnRNB9+PimLrTDaAsS3ROtEHQfUvtlbUGaaxS9VuRo9Nr4/dAaQeemLUM04zFPu/bbp/vXohWCzi31QTB1iqEtxuScHMXXYiqCzi0VdHStKN7JIejOCDq3VNCpfXG0j2bL0RlB55Y64dDWYnLbkYqfp4idEXRuejKYGp1+NLHqA2G0Ov/4fv+eaI2g+zDtoYr2yFHM0SqOmRB0H7RKp86YU6OVmZjnRtB90daibdSszNkQdJ90WtF2FD8fBudG0H3QahudL7eZz+/ie6IVgs5NMUdP//Q3Hem1CZ1v3HVG0LlFwU7ukbW1eChsvTZ+T7RG0DlpuxCNTj2i61MPVjSp9+BBBJ2Tjt4mZ9pqm4qavXQnBJ1TdEynYKNrG9qKRDPtfQgRdE7RtAkzGoLuhKBzimbalkMfEKMh6E4IOqfouE6T2g+njvg0HN11QtA5aVVNjVZqha0VWfQUMfVoXN/Ki+6PqQg6J624s34pKRr+k2xnBJ1b6gv+bUdfPY3ui1YIug+KustKzQfBuRF0X7T9UKDTwtbrWpV5MpgFQT8GnVjoA6ECb+h3TjKyI2hYIWhYIWhYIWhYIWhYIWhYIWhYIWhYIWhYIWhYIWhYIWhYIWhYIWhYIWhYIWhYIWhYIWhYIWhYIWhYIWjksRv87T8gaFghaFghaFghaFghaFghaFghaFghaFghaFghaFghaFghaFghaFghaFj5G/Tp5uBGP9wGFwHLQO3WQV8WZ8PBnn65fr4SXgwsOrU7CrpqWVuOHf1yscUqjeWjZtXuKOiq5eJ4u1iryj5solbthI1Fp0bV6ljMR2q50JwPi/Uq6oP6BWC5VAuyGh7F3Ey5UaxWL7yqPiTuVxdd3XsTsFiuRq1Wzardu4qL4g8kY6nLNMpK6AAAAABJRU5ErkJggg==`,
		`iVBORw0KGgoAAAANSUhEUgAAALQAAABuCAYAAACOaDl7AAAAAXNSR0IArs4c6QAAAARnQU1BAACxjwv8YQUAAAAJcEhZcwAADsMAAA7DAcdvqGQAAAP7SURBVHhe7dy9ThRRHIbxwxZwEfReiPdAYgUhITYGbGxNuADtLezsTKy1tTAUFEBiRUJrAiFSQCI0jPMuM0rW/+zszp5xd988J/kFlj2z1ePJmY811aPYSKsXW4O9i83BQfnzulQAC+y6anVP7VYZP4zLzbRevnk0cgCwHMp21fAw5uHKXMV8tT0obndWivvnqSiABaZG1aqaraI+Pt1Na+l8a7Bbx0zIWDZqto5aLadqHzKsPToAWHRqt1qlD7RC3+kFqzOWldqtgr5Nw19K0URgWdQdEzQsEDSsEDSsEDSsEDSsEDSsEDSsEDSsEDSsEDSsEDSsEDSsEDSsEDSsEDSsEDSsEDSsEDSsEDSsEDSsEDSsEDSsEDSsEDSsEDQWy37wtykQNKwQNKwQNKwQNKwQNKwQNKwQ9P/y6klRvHvxl15H8zATgu6Tov38vih+nhfh0N/1PnFnQ9B9+fC6KH7dVOW2DM379Db+HEyFoPugOLsMop4ZQef25llVZ8eh46PPxUQIOrezk6rMkfHj7GEF1gmhfup1NHR89LmYCEHnpFij8f1bPL8pflbpzgg6p68fqyJHRtNVjP2n1YSRoSsf0Xy0Iuicom1E0+pci45h29EZQecUjbbVtmlVj+aiFUHnFI22oPV+NKK5aEXQOUXj8Es8t9Z0Iqm/R/MxFkHnFO2HdRdw3K1tgs6KoHNq2g9rlY6i1lUOnTRGg6A7Ieicmi7DaWilVtjaMyv8phsr9SDoTgg6t6ZVetpB0J0QdB+0Es86uFvYCUH3RVuLtsdHFX7Tk3nRZ6IVQfdJJ4IKVuHq7p/oJFCxa7+tOdF1aP1DGP0sTISg5y3ac3PruzOCnrfoiTtFHs1FK4KeJ21JoqGvb0Xz0Yqg5yk6IWT/PBOCnqfo2+A6gYzmYiIEPS9N16q5oTITgu6DotRQtNoPP36OQ6+jE0GNti8DoBVB96HpP5YZN7R3rq9NozOCzk0rcJfBlY0sCDq3aYPWysy+ORuC7oO2Dto/j3uWQ+9pzriH/zE1gu6bVl89r/EYK3JvCBpWCBpWCBpWCBpWCBpWCBpWCBpWCBpWCBpWCBpWCBpWCBpWCBpWCBpWCBpWCBpWCBpWCBpWCBpWCBpWCBpWCBpWCBpWCBpWCBpWCBpWCBpWCBpWCBpWCBpWCBpWCBpWCBpWCBpWCBpW/gR9vjW40y/3wSRgGajdKujrdLE5ONCL252VcDKw6NTuMOiyZW059vTiaptVGstHzardYdBly+l0N62VZR/XUat2wsaiU6Nq9VHMJ2o5aVxupvUy6qPqDWC5lAuyGh7GXI9iI62Wb7wsTxIPy0k3/xwELJabYatls2r3oeKUfgOmGmB2iI7z4AAAAABJRU5ErkJggg==`,
	}

	images := make([]image.Image, 0)
	for _, b := range b64 {
		unbased, err := base64.StdEncoding.DecodeString(b)
		if err != nil {
			return nil, err
		}

		r := bytes.NewReader(unbased)
		i, err := png.Decode(r)
		if err != nil {
			return nil, err
		}
		images = append(images, i)
	}
	return images, nil
}
