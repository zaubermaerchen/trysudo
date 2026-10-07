# trysudo v0.1 Specification

## 1. Purpose / Non-goals

`trysudo`はUnix専用のCLIである。対象コマンドについてsudoの事前照会（preflight）を行い、rootとしての実行、または現在の資格情報での直接実行を選択する。

v0.1は小ささ、予測可能性、スクリプトからの利用、二重実行の防止を優先する。バックエンドはsudoのみ、対象ユーザーはroot固定とする。最終実行にはexecによるプロセス置換を使用する。

以下はv0.1の対象外とする。

- doas、`-u`／`--user`、backend指定、verbose。
- sudoオプションのpassthrough。
- shell command string、environment制御、direct経路でのshell fallback。
- sudo認証キャッシュの明示的な操作、`sudo -v`、独自認証。
- 通常のpreflight・対象コマンドへの独自タイムアウト。
- setuid／setgid運用。
- stderr解析による失敗原因の判定。
- 全signal状態の完全保存・復元、全子孫プロセス終了の保証。

WindowsおよびSudo for Windowsは将来的にも対象外とする。doasと`-u`は将来検討とし、v0.1ではそのための汎用拡張機構を作らない。

## 2. CLI

```text
trysudo [-n|--non-interactive] [--] command [args...]
trysudo -h|--help
trysudo --version
```

| オプション | 動作 |
| --- | --- |
| `-n`, `--non-interactive` | 権限変更のための対話的認証をpreflightと実行の両方で禁止する |
| `-h`, `--help` | ヘルプをstdoutへ表示し、終了値0で終了する |
| `--version` | バージョンをstdoutへ表示し、終了値0で終了する |
| `--` | trysudoのオプション解析を終了する |

- 最初のcommandでオプション解析を終了する。以降の引数はtrysudoでは解釈しない。
- commandなし、空のcommand、未知のオプションはusage errorとする。ただしヘルプ・バージョン表示はcommandを要求しない。
- command以外の空の引数は有効であり、そのまま保持する。
- シェルを介さず、commandとargsをargv配列として渡す。
- パイプ、リダイレクト、変数展開、エイリアス、シェル組み込みをtrysudo自身は解釈しない。

```sh
trysudo -n make install     # trysudoの-n
trysudo make -n install     # makeの-n
trysudo -- ./-command arg   # オプション解析を明示終了
```

## 3. Core invariants

以下はsudo固有ではなく、trysudoのコア仕様とする。

1. 対象コマンドの起動は最大1回とする。「必ず1回」ではなく、0回または1回である。
2. 実行経路を確定した後は、別経路へfallbackしない。
3. trysudo自身が中断を観測した後は、対象コマンドを新たに起動しない。
4. preflightは実行成功を保証しない。
5. preflightと実行の原子性は保証しない。

起動回数の保証はtrysudoが実行を依頼する対象コマンドについてのものであり、対象コマンド自身による子プロセス起動を制限するものではない。preflightは対象コマンドを起動しない。

終了値による照会結果判定や出力非解析などの具体的なpreflight契約は、sudo v0.1の規則として定義する。

## 4. Execution model

```text
CLI解析
  ├─ help / version → 表示して終了
  ├─ usage error → 2
  └─ 実行要求
       ↓
資格情報の検査
  ├─ real UID != effective UID → error
  ├─ real GID != effective GID → error
  └─ 通常の資格情報
       ↓
geteuid() == 0 ?
  ├─ yes → direct経路を確定
  └─ no
       ↓
sudoを一度だけPATH探索
  ├─ 利用不能 → direct経路を確定 → fallback通知
  ├─ trysudo内部エラー → error
  └─ 利用可能 → signal監視を開始 → preflight
       ↓
preflight終了結果を暫定的に保持
       ↓
signal通知を解除し、配送済み通知と中断状態を確認
  ├─ 中断観測 / 子のsignal終了 → abort
  ├─ trysudo内部エラー → error
  ├─ preflight起動不能 / 通常非ゼロ終了
  │    → direct経路を確定 → fallback通知
  └─ 通常終了・終了値0 → sudo経路を確定
       ↓
確定した経路でexec
  ├─ exec成功 → 置換先に処理を委ねる
  └─ exec失敗 → 起動エラー。別経路へ戻らない
```

実UIDと実効UID、または実GIDと実効GIDが異なる起動は、v0.1対象外としてエラーにする。この検査後、実効UIDが0ならpreflightなしでdirect実行する。資格情報や環境をsudo相当に初期化し直さない。

将来`-u`を追加しても「rootなら常にdirect」という規則を一般化しない。要求する実行主体と現在の実行主体の関係を改めて扱う。

## 5. sudo preflight

対象コマンドと全引数を含め、次の引数配列でsudoを呼び出す。

| モード | preflight | preflight成功後の最終実行 |
| --- | --- | --- |
| 通常 | `sudo -l -u root -- command args...` | `sudo -u root -- command args...` |
| 非対話 | `sudo -n -l -u root -- command args...` | `sudo -n -u root -- command args...` |

sudo v0.1では以下を適用する。

- 通常終了かつ終了値0だけをpreflight成功とする。中断の有無は別に検査し、中断を優先する。
- stdout・stderrの内容を解析しない。失敗原因を出力から分類しない。
- preflightと実行へ同じcommandとargsを渡す。
- `sudo -v`、`sudo true`などの追加確認を行わない。
- 認証キャッシュを明示的に操作しない。ただしsudo自身による通常の更新は妨げない。
- sudo経路確定後は、sudoへのexec自体の失敗、認証失敗、対象の起動失敗、対象自身の失敗のいずれでもdirectへfallbackしない。

preflight結果には偽陽性・偽陰性があり得る。照会成功を実行権限の予約として扱わない。

## 6. Interactive / non-interactive semantics

### 通常モード

`trysudo command`は対話的認証を許可してsudoの許可照会を行う。照会成功ならsudoに実行を委ね、それ以外の通常失敗なら現在の資格情報で直接実行する。

利用者が端末で操作し、必要ならsudo認証を行う用途を想定する。preflightと実行で再度認証を求められる可能性を許容し、認証回数は保証しない。

最終的に許可されない場合でも認証を求められることがあり、監査ログや認証失敗カウント等へ影響し得る。preflightは副作用のない照会ではない。

Ctrl-Cは「sudoをスキップしてdirect実行する操作」ではなく、trysudo全体の中断である。

### 非対話モード

`trysudo -n command`は、権限変更のための対話的認証をpreflightと実行の両方で禁止する。照会成功なら非対話sudoに実行を委ね、それ以外の通常失敗なら現在の資格情報で直接実行する。

- 対象コマンド自身の対話は禁止しない。
- `sudo -n -l`成功は`sudo -n command`成功を保証しない。
- sudoersの`listpw`等による照会・実行の認証条件の差で、状態変化がなくても照会が成功し、本番が認証不足で毎回失敗する場合がある。
- sudo経路確定後は、その失敗を理由にdirectへ切り替えない。
- 認証サービスへの問い合わせが短時間で終了することは保証しない。

CI、スクリプト、sudo権限が不明な環境、認証待ちを避けたい用途には`-n`を推奨する。TTYの有無からモードを自動変更しない。

## 7. Signal lifecycle

### 7.1 監視開始前

監視対象候補は`SIGINT`、`SIGTERM`、`SIGHUP`、`SIGQUIT`とする。ジョブ制御signalはこの仕組みに含めない。

trysudoが監視を開始する前に現在のignore状態を確認し、ignoreされているsignalは監視対象にしない。残りを`signal.Notify`相当で監視する。

任意のsignal dispositionやmaskの完全保存・復元は保証しない。「監視開始時点」はGoランタイム初期化前の状態を意味しない。

### 7.2 preflight実行中

signal受信と子sudoの終了を監視する。trysudo自身が対象signalを観測したら中断状態を確定し、一度確定した中断状態は解除しない。複数の中断signalを観測した場合、最初に観測したものを終了処理の基準とする。

生存中の子sudoへ同じsignalの転送を試みる。共有プロセスグループ全体へ再送しない。転送の成功は保証しない。

### 7.3 sudo終了後・経路確定前

sudoの終了だけでは直ちにfallbackやexecを確定しない。

- trysudo自身が中断を観測していた場合は対象コマンドを起動しない。
- 子sudoがsignal終了した場合もdirect fallbackしない。
- 通常の終了値が128以上という理由だけでsignal終了と判定しない。
- 最終経路確定前に`signal.Stop`相当で通知を解除する。
- Stop完了後、既に配送済みの通知を確認する。中断が確認された場合は対象を起動しない。
- `signal.Reset()`の一括呼び出しを解除手順の前提にしない。

通知を受信する処理が別に存在する場合、その処理による中断状態の確定も同期する。Wait完了直後のchannel確認だけを通知解除の代わりにしない。

### 7.4 execへの移行

preflight用signal捕捉を残したままexecしない。trysudoが観測した中断後にはexecへ進まない。

捕捉解除後は通常のsignal動作に戻し、exec成功後は置換先に任せる。任意のタイミングのsignalとexecを完全に原子的に順序付けることは保証しない。

### 7.5 中断時の後始末

中断を観測した場合は以下を行う。

1. 生存中の子sudoへ同じsignalの転送を試みる。
2. 短い有限の猶予期間（a short bounded grace period）だけ終了・回収を待つ。
3. 残っていれば強制終了を試みる。
4. 回収可能なら回収する。後始末を無期限に待たない。
5. 対象コマンドを起動せず、終了する。

猶予期間の秒数は公開仕様へ固定しない。この猶予は中断後の後始末だけに適用し、通常のpreflightや対象コマンドへのタイムアウトではない。

以下は保証しない。

- sudoや全子孫プロセスを必ず終了・回収できること。
- signal転送や強制終了が必ず成功すること。
- SIGKILL時の後始末。
- すべての認証helperの回収。
- 子sudoだけが受信し、通常の非ゼロ終了へ変換したsignalの識別。

### 7.6 中断時の終了方法

対応OSで安全に再現できる場合は同一signalによる終了を利用してよい。それ以外は`128 + signal number`で終了してよい。同一signal終了を絶対保証しない。

最終exec成功後の終了状態には、trysudo側の数値変換を適用しない。

## 8. FD / I/O behavior

### preflight

```text
stdin  → /dev/null
stdout → /dev/null
stderr → 呼び出し元stderr
```

- `sudo -S`は使用しない。
- 認証方法はsudoに任せ、askpass等をtrysudo独自に制御しない。
- preflightが対象コマンド用stdinを消費しないことを保証する。
- sudoの診断はstderrへ残す。プロンプトと診断を独自に分離しない。
- 入出力設定はpreflight子に適用し、trysudo自身の元の標準FDを差し替えない。

### 最終実行

元のstdin・stdout・stderrをそのまま継承する。trysudoが開いた不要なFDを最終実行へ漏らさない。

### fallback通知

direct fallback時は、モード・TTYの有無によらずstderrへ短い通知を試みる。

```text
trysudo: sudo preflight unsuccessful; running directly
```

sudoが見つからない場合などは、理由に応じた短い文でよい。出力から推測した失敗理由を断定せず、対象コマンドの全引数を転載しない。rootによる直接実行では通知しない。

通知はbest effortとする。

- 通知失敗を理由に対象コマンドの実行を中止しない。
- stderrがbroken pipeでも、可能な限り予定した実行経路を継続する。
- 通知のためのSIGPIPE設定を対象コマンドへ漏らさない。
- 通知自体の成否で最終終了値を変更しない。

この保証はtrysudo自身の通知に対するものであり、sudoや対象コマンド自身の書込み動作は変更しない。

## 9. PATH / command resolution

### direct経路

- スラッシュを含まないcommandは、現在のPATHで探索する。
- `exec.LookPath`を利用してよいが、`GODEBUG=execerrdot=0`に依存せず、PATH探索結果が相対パスなら拒否する。拒否前に絶対パスへ変換して受け入れてはならない。
- 明示的な`./command`、`../command`、絶対パスは禁止しない。
- direct経路を確定する前に、現在のPATHでの対象不存在を理由に全体を失敗させない。sudo側では見つかる可能性がある。
- direct execでshell fallbackは行わない。

### sudo経路

- command名とargsをそのままsudoへ渡し、探索をsudoに任せる。
- trysudo側で対象コマンドを絶対パスへ正規化しない。
- preflightと実行には同じcommandとargsを渡す。
- 同じファイル実体が実行されることまでは保証しない。

### sudo本体

PATH探索は一度だけ行い、相対的な探索結果を拒否した上で絶対パスを保持する。preflightと本番には同じsudoパスを使用する。パス先のファイルが途中で置換されないことまでは保証しない。

### shebangなしスクリプト

shebangなしスクリプトなど、directでENOEXECとなる対象は終了値126とする。sudo経路ではsudo実装がshell fallbackする場合があり、この経路差は許容する。スクリプトにはshebangを付けるか、明示的にインタープリタを指定する。

## 10. Fallback behavior

| 状況 | 分類・動作 |
| --- | --- |
| sudoが見つからない | direct fallback |
| sudoのPATH探索結果が禁止する相対パス | direct fallback。該当sudoを起動しない |
| preflight用sudoが起動不能：不存在、権限不足、実行形式不正など | direct fallback |
| preflightの通常非ゼロ終了 | direct fallback |
| policy denyによる通常非ゼロ終了 | direct fallback |
| 認証失敗による通常非ゼロ終了 | direct fallback |
| 認証タイムアウト・認証サービス障害による通常非ゼロ終了 | direct fallback |
| sudoers設定エラーによる通常非ゼロ終了 | direct fallback |
| command／args不一致による通常非ゼロ終了 | direct fallback |
| preflightが通常終了・終了値0で、中断なし | sudo path committed |
| trysudoが中断signalを観測 | abort。対象を起動しない |
| preflight子がsignal終了 | abort。directへ進まない |
| 資源不足、待機異常などtrysudo内部エラー | error。directへ進まない |
| sudo経路確定後のsudo exec自体の失敗 | error、126。directへ進まない |

policy deny・認証失敗等は説明上の分類であり、実装では通常非ゼロ終了として共通処理する。stderrや終了値から個別の失敗理由を推測しない。

対象コマンドがpreflight時に見つからず、通常非ゼロ終了となった場合はdirectへ進む。directでも見つからなければ127とする。sudo経路確定後の対象不存在はsudoの結果に任せる。

## 11. Exit status

| 状況 | 終了値・動作 |
| --- | --- |
| help／version | 0 |
| CLI usage error | 2 |
| trysudo内部エラー、未対応の資格情報 | 1 |
| direct command not found | 127 |
| direct command found but exec不可、ENOEXEC等 | 126 |
| directで禁止する相対PATH探索結果を検出 | 126 |
| sudo経路確定後のsudo exec自体の失敗 | 126 |
| 最終exec成功後 | 置換先の終了状態をそのまま利用 |
| preflight中断・子のsignal終了 | signal終了、または`128 + signal number` |

errno等の扱いは次を基本とする。

- directの探索で`exec.ErrNotFound`、または対象パスの不存在を表すENOENT／ENOTDIRを得た場合は127。
- 実行対象を発見・確認した後のexec失敗は126。EACCES、ENOEXEC、E2BIG等を含む。
- 対象を確認した後のexecでENOENTになっても、欠落したshebangインタープリタやローダ等と区別するための追加探索は行わず126とする。
- sudo経路確定後のsudoへのexec失敗は、errnoにかかわらず126。

数値の`128 + signal`とOS上のsignal終了は同一ではない。終了値の数値だけでsignal終了とみなさない。

## 12. Compatibility

### 正式対応の方針

- upstream sudo＋sudoers policyを対象とする。
- 実際にCIまたは実機で検証したOS同梱版を正式対応として記載する。
- 初期検証の中心はsudo 1.9系とする。

### Best effort

- 未検証のupstream sudo。
- sudo-rs。
- 古いsudo。
- 独自policy plugin。

バージョン文字列をruntimeで解析して拒否する機能は作らない。最低sudoバージョンを推測で設定しない。READMEまたは互換性一覧には、実際に検証したOS・アーキテクチャ・sudo実装・バージョンだけを検証済みとして記載する。

本仕様書は実機検証完了を示すものではない。正式な対応OS・アーキテクチャと最低Goバージョンは実装開始時に確定し、検証結果はリリース前に記録する。

## 13. Security / known limitations

- direct fallbackは現在の資格情報で実行する。root実行や特定ユーザーでの実行が必須の用途には使わない。
- 将来`-u`を追加しても、現在の資格情報へのfallbackは権限降格の保証にならない。
- 認証条件の差、認証キャッシュの失効、ポリシー変更、PATH差、ファイル置換による照会と実行の不一致を許容する。
- preflightを子プロセスで行い、最終実行をexecすると親PID関係が変わる。sudoの親PID単位の認証キャッシュを再利用できるとは仮定しない。
- 同名commandでもdirectとsudoで別の実行ファイルが選ばれ得る。特定のパスを要求する場合は利用者が絶対パスを指定する。
- シェル文字列を自動生成しない。ただしsudo実装自身によるENOEXEC時のshell fallbackを禁止するものではない。
- sudoの存在やpreflight成功だけで、実際の起動成功を保証しない。
- 中断保証はtrysudo自身が観測した中断を基準とする。signal送信時刻とexecの厳密な前後関係、子だけに届いた中断の識別、完全なプロセスツリー管理は保証しない。

## 14. Go implementation constraints

- 基本は標準ライブラリだけで実装し、CLIフレームワークは導入しない。
- preflightは`os/exec`で起動し、終了待機・回収を行う。中断時の後始末には第7節の限界を適用する。
- 最終置換は`syscall.Exec`を使用する。Execだけのために`golang.org/x/sys`を追加しない。
- 実行パス、`argv[0]`を含む引数配列、環境変数を正しく渡す。trysudo自身のargvをsudoのargvとして流用しない。
- 生のexecveシステムコールを直接呼ばない。
- exec成功時は戻らず、`defer`も実行されない。必要な通知・後処理はexec前に完了する。
- signal配送の停止と配送済み通知の確認を同期し、確認だけで競合を解消したとみなさない。
- fallback通知の内部手法は限定しないが、通知の書込み失敗やSIGPIPE対策が主処理・置換先の動作を変えないようにする。

## 15. Test requirements

起動時にカウンターを増やす対象コマンド等を用い、全経路で対象起動回数が0または1であることを検証する。

### 実行経路

- preflightが対象コマンドを起動しないこと。
- preflight成功・通常非ゼロ終了・起動不能。
- sudoなし、sudoers deny、command／args不一致、root direct。
- sudo経路確定後のexec失敗、認証失敗、対象コマンドの失敗でdirectへ戻らないこと。
- 対象が副作用を起こしてから非ゼロ終了する場合も再実行しないこと。

### signal

- 実sudo 1.9系の認証中にCtrl-Cを押してもdirectへ進まないこと。
- 子が中断を通常のexit 1へ変換しても、親が中断を観測していれば起動しないこと。
- SIGTERM等をtrysudoだけへ送った場合の転送・後始末。
- sudo終了、signal到着、通知停止、exec移行の競合。
- 監視開始時にignoreされていたsignal。
- 子sudoのsignal終了、転送失敗、子が終了しない場合の有限な後始末。
- exec成功後の終了値・signal状態をtrysudoが変換しないこと。

### 入出力・認証

- preflightが対象コマンド用stdinを消費しないこと。
- preflight stdoutを破棄し、stderrを継承すること。
- stderr broken pipeでもtrysudoのfallback通知が主処理を阻害しないこと。
- 通知のSIGPIPE対策が置換先へ漏れないこと。
- `-n`を照会・実行の両方へ渡すこと。
- `listpw`等による、状態変化のない非対話preflightのfalse positive。

### 引数・探索・終了値

- command以降のオプション、`--`、空の引数、空白、日本語の保持。
- help、version、usage error。
- directとsudoのPATH差。
- ErrDot、および`GODEBUG=execerrdot=0`時の相対PATH探索結果の拒否。
- 明示的な`./command`、`../command`の許可。
- shebangなしスクリプトの経路差。
- 対象不存在、権限不足、ENOEXEC、欠落したインタープリタ等の126／127。
- sudo本体の探索が一度だけで、照会と実行に同じパスを渡すこと。

sudo-rsの検証結果はupstream sudoと分けて記録し、v0.1ではbest effortとして扱う。
