package model

// TaskMarkerPrefix は、タスクをプロンプトとしてコピーするときに最終行へ添える marker の接頭辞。
// 次の PR で、セッションの最初のプロンプトからこの marker を拾ってタスクの下へ自動で付ける。
// Why: 形式をここで先に固定するのは、UI（JS 側に同じ書式を持つ）と後段の検出が食い違わないようにするため。
const TaskMarkerPrefix = "[devctx:task:"

// TaskMarker は id のタスクを指す marker（例: "[devctx:task:t3]"）を返す。
func TaskMarker(id string) string { return TaskMarkerPrefix + id + "]" }
