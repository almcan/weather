package main

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"time"
)

// --- 気象庁JSONの構造体定義 ---
type AreaInfo struct {
	Area struct {
		Name string `json:"name"`
		Code string `json:"code"`
	} `json:"area"`
	WeatherCodes []string `json:"weatherCodes,omitempty"`
	Weathers     []string `json:"weathers,omitempty"`
	Winds        []string `json:"winds,omitempty"`
	Waves        []string `json:"waves,omitempty"`
	Pops         []string `json:"pops,omitempty"`
	Temps        []string `json:"temps,omitempty"`
}
type TimeSeriesInfo struct {
	// JSONの時刻文字列をそのまま受け取る
	TimeDefines []string   `json:"timeDefines"`
	Areas       []AreaInfo `json:"areas"`
}
type Forecast struct {
	PublishingOffice string `json:"publishingOffice"`
	// time.Time型で受け取る
	ReportDatetime time.Time        `json:"reportDatetime"`
	TimeSeries     []TimeSeriesInfo `json:"timeSeries"`
	// tempAverage, precipAverage など他のキーは今回は省略
}

// 日本の主要都市のエリアコードリスト (札幌、那覇は都道府県コードのまま)
var areaCodes = []string{
	"130000", // 東京
	"270000", // 大阪
	"016000", // 札幌
	"040000", // 仙台
	"230000", // 名古屋
	"330000", // 岡山
	"400000", // 福岡
	"471000", // 那覇
}

// WeatherResponse を複数都市対応に変更
type WeatherResponse struct {
	CityWeather []CityWeather `json:"cityWeather"`
	LastError   string        `json:"lastError,omitempty"` // データ取得中に最後に発生したエラー（参考情報）
}

// CityWeather は各都市の天気情報を保持する構造体
type CityWeather struct {
	AreaCode        string   `json:"areaCode"`        // リクエストに使ったエリアコード
	ReportTime      string   `json:"reportTime"`      // 発表日時
	AreaName        string   `json:"areaName"`        // 予報区名 (例: "東京地方", "大阪府")
	TodayWeather    string   `json:"todayWeather"`    // 今日の天気
	TomorrowWeather string   `json:"tomorrowWeather"` // 明日の天気
	TempAreaName    string   `json:"tempAreaName"`    // 気温地点名 (例: "東京", "大阪")
	TempTodayHigh   string   `json:"tempTodayHigh"`   // 今日の最高気温
	TempTmrwLow     string   `json:"tempTmrwLow"`     // 明日の最低気温
	TempTmrwHigh    string   `json:"tempTmrwHigh"`    // 明日の最高気温
	Pops            []string `json:"Pops"`            // 降水確率
	Winds           []string `json:"Winds"`           // 風
	Error           string   `json:"error,omitempty"` // エラー情報 (あれば)
}

// グローバル変数で天気データを保持
var currentWeatherData WeatherResponse
var lastFetchedTime time.Time

func fetchWeatherData() {
	log.Println("気象庁から主要都市の天気データを取得・更新します...")

	var cityWeatherList []CityWeather
	var lastErrorMsg string // ループ中の最後のエラーを記録する変数

	for _, areaCode := range areaCodes {
		log.Printf("エリアコード [%s] のデータを取得中...", areaCode)
		url := fmt.Sprintf("https://www.jma.go.jp/bosai/forecast/data/forecast/%s.json", areaCode)

		resp, err := http.Get(url)
		if err != nil {
			errMsg := fmt.Sprintf("リクエスト失敗 (エリア %s): %v", areaCode, err)
			log.Println(errMsg)
			lastErrorMsg = errMsg // 最後のエラーを更新
			continue              // 次のエリアコードへ
		}

		// ステータスコードチェック (404 Not Found など)
		if resp.StatusCode != http.StatusOK {
			errMsg := fmt.Sprintf("APIエラー応答 (エリア %s): ステータス %d", areaCode, resp.StatusCode)
			log.Println(errMsg)
			resp.Body.Close() // エラー時もBodyを閉じる
			lastErrorMsg = errMsg
			continue
		}

		bodyBytes, err := ioutil.ReadAll(resp.Body)
		resp.Body.Close() // ReadAllの直後にBodyを閉じる (deferを使わない)
		if err != nil {
			errMsg := fmt.Sprintf("レスポンス読み込み失敗 (エリア %s): %v", areaCode, err)
			log.Println(errMsg)
			lastErrorMsg = errMsg
			continue
		}

		var forecasts []Forecast
		err = json.Unmarshal(bodyBytes, &forecasts)
		if err != nil {
			errMsg := fmt.Sprintf("JSONデコード失敗 (エリア %s): %v", areaCode, err)
			log.Println(errMsg)
			lastErrorMsg = errMsg
			continue
		}

		// --- データ抽出処理 ---
		if len(forecasts) > 0 {
			todayForecast := forecasts[0]
			// この都市の天気情報を格納する変数
			cityWeather := CityWeather{
				AreaCode:   areaCode, // リクエストしたエリアコードを記録
				ReportTime: todayForecast.ReportDatetime.Format("2006-01-02 15:04"),
			}

			// 天気情報の取得: TimeSeries[0] の Areas[0] (最初の予報区) を使う
			if len(todayForecast.TimeSeries) > 0 && len(todayForecast.TimeSeries[0].Areas) > 0 {
				areaWeather := todayForecast.TimeSeries[0].Areas[0]
				cityWeather.AreaName = areaWeather.Area.Name // 予報区名をセット
				if len(areaWeather.Weathers) > 0 {
					cityWeather.TodayWeather = areaWeather.Weathers[0]
				}
				if len(areaWeather.Weathers) > 1 {
					cityWeather.TomorrowWeather = areaWeather.Weathers[1]
				}
				// 風情報の取得: TimeSeries[0] の Areas[0] を使う (最初の予報区)
				if len(areaWeather.Winds) > 0 {
					cityWeather.Winds = areaWeather.Winds // 風情報をセット (最初の要素を使用)
				}
			} else {
				log.Printf("注意: エリア [%s] で天気情報が見つかりません (TimeSeries[0]/Areas[0])。", areaCode)
			}

			// 気温情報の取得: TimeSeriesの中から Temps が存在する最初の Area を使う
			foundTemp := false
			for _, ts := range todayForecast.TimeSeries {
				for _, area := range ts.Areas {
					if len(area.Temps) > 0 { // Temps配列が存在するかチェック
						cityWeather.TempAreaName = area.Area.Name // 地点名をセット
						// Temps配列の要素数をチェックしながらアクセス
						if len(area.Temps) > 1 {
							cityWeather.TempTodayHigh = area.Temps[1]
						}
						if len(area.Temps) > 2 {
							cityWeather.TempTmrwLow = area.Temps[2]
						}
						if len(area.Temps) > 3 {
							cityWeather.TempTmrwHigh = area.Temps[3]
						}
						foundTemp = true
						break // 最初に見つかった気温情報を採用
					}
				}
				if foundTemp {
					break
				} // 内側のループで見つかったら外側も抜ける
			}
			if !foundTemp {
				log.Printf("注意: エリア [%s] で気温情報 (Temps) が見つかりません。", areaCode)
			}

			foundPops := false
			// TimeSeries 配列をループして Pops を探す
			for _, ts := range todayForecast.TimeSeries {
				if len(ts.Areas) > 0 && len(ts.Areas[0].Pops) > 0 {
					cityWeather.Pops = ts.Areas[0].Pops
					foundPops = true
					break
				}
			}
			if !foundPops {
				log.Printf("注意: エリア [%s] で降水確率情報 (Pops) が見つかりません。", areaCode)
			}

			// 抽出した都市の天気情報をリストに追加
			cityWeatherList = append(cityWeatherList, cityWeather)
			log.Printf("エリア [%s] (%s) のデータ抽出完了", areaCode, cityWeather.AreaName)

		} else {
			// forecasts 配列が空だった場合 (通常は起こりにくいが念のため)
			errMsg := fmt.Sprintf("データ形式エラー (エリア %s): 予報情報が見つかりません", areaCode)
			log.Println(errMsg)
			lastErrorMsg = errMsg
		}
	} // エリアコードのループ終了

	// グローバル変数を更新 (取得できた都市のリストと、最後に記録されたエラー)
	currentWeatherData = WeatherResponse{
		CityWeather: cityWeatherList,
		LastError:   lastErrorMsg, // 参考情報として最後のエラーを格納
	}
	lastFetchedTime = time.Now()
	log.Printf("天気データの取得・更新完了。%d / %d 都市のデータを取得しました。", len(cityWeatherList), len(areaCodes))
}

// weatherHandler は、グローバル変数 currentWeatherData をJSONで返すだけ
func weatherHandler(w http.ResponseWriter, r *http.Request) {
	log.Printf("リクエスト受信: %s %s From: %s", r.Method, r.URL.Path, r.RemoteAddr)

	// --- CORSヘッダー ---
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Methods", "GET, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusOK)
		return
	}
	// --- CORS ここまで ---

	w.Header().Set("Content-Type", "application/json; charset=utf-8")

	// グローバル変数 currentWeatherData をエンコードして返す
	err := json.NewEncoder(w).Encode(currentWeatherData)
	if err != nil {
		log.Printf("レスポンスのJSONエンコードに失敗: %v", err)
		http.Error(w, `{"error":"サーバー内部エラー"}`, http.StatusInternalServerError)
	}
}

// --- 定期的に天気データを取得する関数 ---
func startWeatherDataFetcher(interval time.Duration) {
	log.Printf("天気データの自動更新処理を開始します。更新間隔: %v", interval)

	// 指定された間隔で通知を送るタイマーを作成
	ticker := time.NewTicker(interval)
	// 関数終了時にタイマーを停止する (丁寧な後片付け)
	defer ticker.Stop()

	// 無限ループ: ticker.C チャネルから通知が来るのを待つ
	for {
		// <-ticker.C は、タイマーが次の時間になるまでここで待機し、
		// 時間が来たら通知 (現在時刻) を受け取る、という動きをします。
		t := <-ticker.C
		log.Printf("定刻 (%v)。天気データを更新します...", t.Format("15:04:05"))
		fetchWeatherData() // データを取得・更新する関数を呼び出す
	}
	// 基本的にこのループから抜けることはありません (サーバーが動いている限り)
}

func main() {
	// 1. サーバー起動時にまず一度、天気データを取得する (初期データ)
	log.Println("サーバー起動処理開始。初期の天気データを取得します...")
	fetchWeatherData()

	// 2. バックグラウンドで定期的なデータ更新処理を開始する (Goroutine)
	updateInterval := 1 * time.Hour            // 1時間ごとに設定
	go startWeatherDataFetcher(updateInterval) // "go" キーワードでバックグラウンド実行を開始

	// 3. HTTPリクエストを処理するハンドラを登録
	http.HandleFunc("/api/weather", weatherHandler)

	// 4. Webサーバーを起動
	port := "8080"
	fmt.Printf("天気予報APIサーバーをポート %s で起動します...\n", port)
	fmt.Println("----------------------------------------------------")
	fmt.Printf("GoバックエンドAPIのURL: http://localhost:%s/api/weather\n", port)
	fmt.Printf("データは %v ごとに自動で更新されます。\n", updateInterval) // 更新間隔を表示
	fmt.Println("Ctrl+C でサーバーを停止します。")
	fmt.Println("----------------------------------------------------")

	// サーバーを起動し、リクエストを待ち続ける
	log.Fatal(http.ListenAndServe(":"+port, nil))
}
