/**
 * ==============================================================================
 * 🎵 BetterLyrics Google Apps Script (GAS) Edge Accelerator Node
 * ==============================================================================
 * Petunjuk Deployment:
 * 1. Buka https://script.google.com/ dan buat "New Project".
 * 2. Salin seluruh isi file ini ke dalam editor code Google Apps Script (Code.gs).
 * 3. Klik "Deploy" -> "New deployment" -> Pilih tipe "Web app".
 * 4. Atur:
 *    - Execute as: "Me" (email Anda)
 *    - Who has access: "Anyone" (Anonim / Siapapun)
 * 5. Klik "Deploy" dan salin Web App URL (akhiran /exec).
 * 6. Pasang URL tersebut ke .env Origin Anda:
 *    NODE_GAS=https://script.google.com/macros/s/XXXXXX/exec
 * ==============================================================================
 */

// Konfigurasi Default
const CONFIG = {
  UPSTREAM_URL: "https://lyrics.api.dacubeking.com",
  CACHE_TTL_SECONDS: 21600, // 6 jam (batas maksimal per item di Google CacheService adalah 6 jam / 21600 detik)
  VERSION: "1.0.0-gas",
};

/**
 * Handle HTTP GET Requests (/interconnect, /health, /ping)
 */
function doGet(e) {
  const path = (e && e.parameter && e.parameter.path) || (e && e.pathInfo) || "interconnect";
  
  if (path === "health" || path === "ping") {
    return ContentService.createTextOutput("OK").setMimeType(ContentService.MimeType.TEXT);
  }

  // Default: Interconnect Telemetry Data
  const cache = CacheService.getScriptCache();
  const hits = parseInt(cache.get("gas_stats_hits") || "0", 10);
  const misses = parseInt(cache.get("gas_stats_misses") || "0", 10);
  const total = hits + misses;
  const hitRate = total > 0 ? ((hits / total) * 100).toFixed(2) + "%" : "0%";

  const telemetry = {
    status: "online",
    role: "edge_node",
    platform: "GOOGLE_APPS_SCRIPT",
    version: CONFIG.VERSION,
    cache: {
      provider: "Google CacheService",
      hits: hits,
      misses: misses,
      hit_rate: hitRate,
      max_ttl_seconds: CONFIG.CACHE_TTL_SECONDS
    },
    timestamp: Math.floor(Date.now() / 1000),
    server_time: new Date().toISOString()
  };

  return createJsonResponse(telemetry);
}

/**
 * Handle HTTP POST Requests (/v2/lyrics, /verify-turnstile)
 */
function doPost(e) {
  try {
    const path = (e && e.parameter && e.parameter.path) || (e && e.pathInfo) || "v2/lyrics";
    const postData = (e && e.postData && e.postData.contents) || "";
    
    // Proxy /verify-turnstile
    if (path.includes("verify-turnstile")) {
      const resp = UrlFetchApp.fetch(CONFIG.UPSTREAM_URL + "/verify-turnstile", {
        method: "post",
        contentType: "application/json",
        payload: postData,
        muteHttpExceptions: true
      });
      return ContentService.createTextOutput(resp.getContentText()).setMimeType(ContentService.MimeType.JSON);
    }

    // Proxy /v2/lyrics with Caching
    const params = parseFormData(postData);
    const videoId = params.videoId || "";
    const song = params.song || "";
    const artist = params.artist || "";
    const duration = params.duration || "";
    const isrc = params.isrc || "";

    const cacheKey = "bl_" + Utilities.base64Encode(
      Utilities.computeDigest(
        Utilities.DigestAlgorithm.SHA_256,
        (videoId + "|" + song + "|" + artist + "|" + duration + "|" + isrc).toLowerCase()
      )
    ).replace(/[^a-zA-Z0-9_]/g, "").substring(0, 32);

    const scriptCache = CacheService.getScriptCache();
    const cachedLyrics = scriptCache.get(cacheKey);

    if (cachedLyrics) {
      incrementStat("gas_stats_hits");
      return ContentService.createTextOutput(cachedLyrics)
        .setMimeType(ContentService.MimeType.TEXT);
    }

    incrementStat("gas_stats_misses");

    // Fetch from official upstream
    const response = UrlFetchApp.fetch(CONFIG.UPSTREAM_URL + "/v2/lyrics", {
      method: "post",
      contentType: "application/x-www-form-urlencoded",
      payload: postData,
      muteHttpExceptions: true
    });

    const responseCode = response.getResponseCode();
    const responseText = response.getContentText();

    // Cache successful lyrics stream
    if (responseCode === 200 && responseText && responseText.indexOf("event:") !== -1) {
      try {
        scriptCache.put(cacheKey, responseText, CONFIG.CACHE_TTL_SECONDS);
      } catch (err) {
        // Abaikan jika item melebihi batas ukuran CacheService (~100KB)
      }
    }

    return ContentService.createTextOutput(responseText)
      .setMimeType(ContentService.MimeType.TEXT);

  } catch (error) {
    return createJsonResponse({
      error: true,
      message: error.toString()
    });
  }
}

/**
 * Utility: Parse URL-encoded Form Data
 */
function parseFormData(queryString) {
  const result = {};
  if (!queryString) return result;
  const pairs = queryString.split("&");
  for (let i = 0; i < pairs.length; i++) {
    const part = pairs[i].split("=");
    if (part.length === 2) {
      const key = decodeURIComponent(part[0].replace(/\+/g, " "));
      const val = decodeURIComponent(part[1].replace(/\+/g, " "));
      result[key] = val;
    }
  }
  return result;
}

/**
 * Utility: Create JSON Response with CORS Header simulation
 */
function createJsonResponse(data) {
  return ContentService.createTextOutput(JSON.stringify(data))
    .setMimeType(ContentService.MimeType.JSON);
}

/**
 * Utility: Increment Metric in Cache
 */
function incrementStat(key) {
  try {
    const cache = CacheService.getScriptCache();
    const count = parseInt(cache.get(key) || "0", 10) + 1;
    cache.put(key, count.toString(), 21600);
  } catch (e) {}
}
