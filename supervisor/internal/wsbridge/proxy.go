package wsbridge

// The tool iframe keeps allow-same-origin because the shipped pack is trusted,
// immutable first-party code and its htmx requests must retain same-origin
// credentials. This is an accepted residual risk: HttpOnly session cookies,
// same-site/origin checks, frame-ancestors CSP, and header stripping still
// close the cross-site attack path. Pack-controlled or untrusted code is not a
// supported trust model for this v1 boundary.
//
// JS-generated absolute URLs are explicitly unsupported in v1. Pack GUI code
// must use relative URLs or markup attributes that this proxy can rewrite.

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"mime"
	"net"
	"net/http"
	"net/http/httputil"
	"path"
	"strconv"
	"strings"

	"github.com/rohanpunj/iolbox/supervisor/internal/tool"
	"golang.org/x/net/html"
)

const toolHTMLRewriteCap = 2 << 20

func (b *Bridge) handleTool(w http.ResponseWriter, r *http.Request) {
	nodeID, upstreamPath, err := toolProxyPath(r.URL.Path)
	if err != nil || toolPathHasTraversal(r.URL.Path, r.URL.RawPath) {
		http.NotFound(w, r)
		return
	}
	socket, routes, ok := b.ctrl.ToolProxyTarget(nodeID)
	if !ok || socket == "" {
		http.NotFound(w, r)
		return
	}
	route, ok := matchingToolRoute(routes, upstreamPath)
	if !ok {
		http.NotFound(w, r)
		return
	}
	if isWebSocketUpgrade(r) && !route.AllowWS {
		http.Error(w, "websocket route is not allowlisted", http.StatusForbidden)
		return
	}

	prefix := "/tool/" + strconv.Itoa(nodeID) + "/"
	proxy := &httputil.ReverseProxy{
		Rewrite: func(request *httputil.ProxyRequest) {
			request.Out.URL.Scheme = "http"
			request.Out.URL.Host = "iolbox-tool"
			request.Out.URL.Path = upstreamPath
			request.Out.URL.RawPath = ""
			request.Out.Host = "iolbox-tool"
			request.Out.RequestURI = ""
			request.Out.Header.Del("Accept-Encoding")
			stripForwardedHeaders(request.Out.Header)
			stripSessionCookie(request.Out.Header)
		},
		Transport: &http.Transport{
			DisableCompression: true,
			DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
				return (&net.Dialer{}).DialContext(ctx, "unix", socket)
			},
		},
		ModifyResponse: func(response *http.Response) error {
			if response.StatusCode >= http.StatusMultipleChoices && response.StatusCode < http.StatusBadRequest {
				if location := response.Header.Get("Location"); location != "" {
					response.Header.Set("Location", rewriteRootURL(location, prefix))
				}
			}
			if isHTMLResponse(response) {
				response.Header.Set("Content-Security-Policy", "frame-ancestors 'self'")
			}
			return rewriteHTMLResponse(response, prefix)
		},
		ErrorHandler: func(response http.ResponseWriter, _ *http.Request, proxyErr error) {
			http.Error(response, fmt.Sprintf("tool proxy: %v", proxyErr), http.StatusBadGateway)
		},
	}
	proxy.ServeHTTP(w, r)
}

func toolProxyPath(requestPath string) (int, string, error) {
	const root = "/tool/"
	if !strings.HasPrefix(requestPath, root) {
		return 0, "", fmt.Errorf("not a tool path")
	}
	rest := strings.TrimPrefix(requestPath, root)
	slash := strings.IndexByte(rest, '/')
	nodePart := rest
	upstreamPath := "/"
	if slash >= 0 {
		if slash == 0 {
			return 0, "", fmt.Errorf("missing node id")
		}
		nodePart = rest[:slash]
		upstreamPath = rest[slash:]
	}
	nodeID, err := strconv.Atoi(nodePart)
	if err != nil || nodeID < 0 {
		return 0, "", fmt.Errorf("invalid node id")
	}
	clean := path.Clean(upstreamPath)
	if toolPathHasTraversal(upstreamPath) || !strings.HasPrefix(clean, "/") {
		return 0, "", fmt.Errorf("invalid tool path")
	}
	return nodeID, clean, nil
}

func matchingToolRoute(routes []tool.ProxyRoute, requestPath string) (tool.ProxyRoute, bool) {
	var matched tool.ProxyRoute
	matchedLength := -1
	for _, route := range routes {
		prefix := path.Clean(route.Prefix)
		if prefix == "/" || requestPath == prefix || strings.HasPrefix(requestPath, prefix+"/") {
			if len(prefix) > matchedLength {
				matched = route
				matchedLength = len(prefix)
			}
		}
	}
	return matched, matchedLength >= 0
}

func isWebSocketUpgrade(r *http.Request) bool {
	return strings.EqualFold(strings.TrimSpace(r.Header.Get("Upgrade")), "websocket")
}

func stripForwardedHeaders(header http.Header) {
	for key := range header {
		lower := strings.ToLower(key)
		if lower == "forwarded" || strings.HasPrefix(lower, "x-forwarded-") {
			delete(header, key)
		}
	}
}

func stripSessionCookie(header http.Header) {
	cookies := (&http.Request{Header: header}).Cookies()
	header.Del("Cookie")
	for _, cookie := range cookies {
		if !strings.EqualFold(cookie.Name, "iolbox_session") {
			header.Add("Cookie", cookie.Name+"="+cookie.Value)
		}
	}
}

func isHTMLResponse(response *http.Response) bool {
	mediaType, _, err := mime.ParseMediaType(response.Header.Get("Content-Type"))
	return err == nil && strings.EqualFold(mediaType, "text/html")
}

func rewriteHTMLResponse(response *http.Response, prefix string) error {
	if !isHTMLResponse(response) || response.Header.Get("Content-Encoding") != "" || response.ContentLength > toolHTMLRewriteCap {
		return nil
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, toolHTMLRewriteCap+1))
	if err != nil {
		return err
	}
	if len(body) > toolHTMLRewriteCap {
		response.Body = io.NopCloser(io.MultiReader(bytes.NewReader(body), response.Body))
		return nil
	}
	response.Body.Close()
	rewritten, err := rewriteHTML(body, prefix)
	if err != nil {
		return err
	}
	response.Body = io.NopCloser(bytes.NewReader(rewritten))
	response.ContentLength = int64(len(rewritten))
	response.Header.Set("Content-Length", strconv.Itoa(len(rewritten)))
	response.Header.Del("Transfer-Encoding")
	response.TransferEncoding = nil
	return nil
}

func rewriteHTML(body []byte, prefix string) ([]byte, error) {
	tokenizer := html.NewTokenizer(bytes.NewReader(body))
	var output bytes.Buffer
	for {
		tokenType := tokenizer.Next()
		if tokenType == html.ErrorToken {
			if tokenizer.Err() == io.EOF {
				return output.Bytes(), nil
			}
			return nil, tokenizer.Err()
		}
		token := tokenizer.Token()
		if tokenType == html.StartTagToken || tokenType == html.SelfClosingTagToken {
			for index := range token.Attr {
				name := strings.ToLower(token.Attr[index].Key)
				if name == "href" && strings.EqualFold(token.Data, "base") {
					token.Attr[index].Val = prefix
				} else if name == "href" || name == "src" || name == "action" || strings.HasPrefix(name, "hx-") {
					token.Attr[index].Val = rewriteRootURL(token.Attr[index].Val, prefix)
				}
			}
		}
		output.WriteString(token.String())
	}
}

func rewriteRootURL(value, prefix string) string {
	if value == "" || !strings.HasPrefix(value, "/") || strings.HasPrefix(value, "//") || strings.HasPrefix(value, prefix) {
		return value
	}
	return prefix + strings.TrimPrefix(value, "/")
}
