package roadmap

import (
	"fmt"
	"mime"
	"net/http"
)

// guard は mux 全体を包み、localhost 向けの Web サーバに他サイトのページから届くリクエストを落とす。
//
//   - Host: 127.0.0.1:<port> / localhost:<port> 以外は 403。DNS rebinding（攻撃者のドメインが
//     127.0.0.1 に解決される）では Host が攻撃者のドメインのままなので、GET でも弾く。
//   - 更新系（GET / HEAD 以外）: Content-Type が application/json、かつ Origin が "http://"+Host と完全一致。
//     Why: 他サイトの <form> は JSON を送れず、fetch で JSON を送るには CORS preflight が要る（この
//     サーバは preflight に応答しない）。Origin 一致の確認は、それでも届いたリクエストへの二重の備え。
//
// Why not CSRF トークン: 状態を持たない単一ユーザーの localhost ツールで、Origin 検査だけで足りる。
// Why not [::1] を許可: 待ち受けが 127.0.0.1 だけなので、到達できないホスト名を許可する意味がない。
func (s *Server) guard(next http.Handler) http.Handler {
	allowedHosts := map[string]bool{
		fmt.Sprintf("127.0.0.1:%d", s.Port): true,
		fmt.Sprintf("localhost:%d", s.Port): true,
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !allowedHosts[r.Host] {
			http.Error(w, "forbidden host", http.StatusForbidden)
			return
		}
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			// Origin が "null"（sandbox iframe 等）や欠落のものも、Host との完全一致で一括して落ちる
			if origin := r.Header.Get("Origin"); origin == "" || origin != "http://"+r.Host {
				http.Error(w, "forbidden origin", http.StatusForbidden)
				return
			}
			mt, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
			if err != nil || mt != "application/json" {
				http.Error(w, "content type must be application/json", http.StatusUnsupportedMediaType)
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}
