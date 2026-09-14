(() => {
  const node = id => document.getElementById(id);
  const panel = node('playerPanel');
  const video = node('onlineVideo');
  const episodeList = node('playbackEpisodes');
  const statusText = node('playbackStatus');
  const errorText = node('playbackError');
  let dramaID = '';
  let dramaName = '';
  let collectionTaskID = '';
  let collectionMode = false;
  let preparedIndex = 0;
  let sessionID = '';
  let sessionAvailable = false;
  let mimeType = '';
  let episodes = [];
  let episodeButtons = [];
  let currentIndex = 0;
  let openingVersion = 0;
  let streamVersion = 0;
  let openingController = null;
  let streamController = null;
  let objectURL = '';
  let loading = false;
  let heartbeatTimer = null;
  let heartbeatPending = false;
  let seekTimer = null;
  let lastPosition = 0;
  let playbackRun = 0;
  let streamComplete = false;
  let prefetchAttempted = 0;
  let prefetchVersion = 0;
  let historySource = '';
  let historySequence = 0;
  let historyPlayed = false;
  let historyCompleted = false;
  let historySentAt = 0;
  let historySignature = '';
  let playbackDuration = 0;
  let historyMessage = '';
  const historyStatus = node('playbackHistoryStatus');
  const prefetchToggle = node('prefetchNextEpisode');
  const prefetchStatus = node('prefetchStatus');
  try {prefetchToggle.checked = localStorage.getItem('juku.playback.prefetchNext') !== 'false';} catch (_) {}

  function clear(element) {
    while (element.firstChild) element.removeChild(element.firstChild);
  }

  async function requestJSON(path, body, signal) {
    const response = await fetch(path, {
      method: body === undefined ? 'GET' : 'POST',
      headers: {'Accept': 'application/json', 'Content-Type': 'application/json'},
      body: body === undefined ? undefined : JSON.stringify(body),
      signal
    });
    const result = await response.json();
    if (!response.ok) {
      const error = new Error(result.error || 'HTTP ' + response.status);
      error.status = response.status;
      throw error;
    }
    return result;
  }

  function historySnapshot(force = false, repeat = false) {
    if (!historyPlayed || !sessionID || !playbackRun || !episodes[currentIndex - 1] || !window.JukuHistory) return null;
    const position = !loading && Number.isFinite(video.currentTime) ? video.currentTime : lastPosition;
    const duration = Number.isFinite(video.duration) && video.duration > 0 ? video.duration : playbackDuration;
    if (!Number.isFinite(position) || position < 0) return null;
    const signature = playbackRun + ':' + position.toFixed(3) + ':' + historyCompleted;
    if (!repeat && signature === historySignature || !force && Date.now() - historySentAt < 10000) return null;
    historySignature = signature;
    historySentAt = Date.now();
    const episode = episodes[currentIndex - 1];
    const progress = {run: playbackRun, sequence: ++historySequence, episode: currentIndex, position, duration: duration || 0, completed: historyCompleted};
    const entry = {dramaId: dramaID, title: dramaName, source: historySource, chapterId: episode.chapterId || '', episode: episode.episode, index: episode.number || currentIndex, total: episode.total || episodes.length, position, duration: duration || 0, completed: historyCompleted, mode: collectionMode ? 'collection' : 'online', taskId: episode.taskId || '', watchedAt: new Date().toISOString()};
    return {progress, entry};
  }

  function saveHistory(force = false) {
    const snapshot = historySnapshot(force);
    if (!snapshot) return;
    const session = sessionID;
    window.JukuHistory.save(session, snapshot.progress, snapshot.entry).then(() => {
      if (session === sessionID && snapshot.progress.sequence === historySequence) historyStatus.textContent = historyMessage;
    }).catch(error => {
      if (session !== sessionID || snapshot.progress.sequence !== historySequence) return;
      historySignature = '';
      historyStatus.textContent = '观看进度暂未保存：' + error.message;
    });
  }

  function releaseSession(id, progress, unloading = false) {
    if (!id) return;
    const body = JSON.stringify({session: id, action: 'close', ...(progress ? {progress} : {})});
    if (unloading && navigator.sendBeacon && navigator.sendBeacon('/api/ui/playback/control', new Blob([body], {type: 'application/json'}))) return;
    fetch('/api/ui/playback/control', {method: 'POST', headers: {'Content-Type': 'application/json'}, body, keepalive: true}).then(response => response.json()).then(result => {
      if (progress && result.historyError) window.JukuHistory?.reportError(result.historyError);
    }).catch(error => {if (progress && !unloading) window.JukuHistory?.reportError(error.message);});
  }

  function stopStream() {
    saveHistory(true);
    historyPlayed = false;
    historyCompleted = false;
    historySignature = '';
    playbackDuration = 0;
    window.JukuPlaybackDanmaku?.suspend();
    streamVersion++;
    playbackRun = 0;
    streamComplete = false;
    prefetchAttempted = 0;
    prefetchVersion++;
    prefetchStatus.hidden = true;
    prefetchStatus.textContent = '';
    loading = true;
    clearTimeout(seekTimer);
    if (streamController) streamController.abort();
    streamController = null;
    video.pause();
    video.removeAttribute('src');
    video.load();
    if (objectURL) URL.revokeObjectURL(objectURL);
    objectURL = '';
  }

  function dispose(unloading = false) {
    const snapshot = historySnapshot(true, true);
    if (snapshot) window.JukuHistory.remember(snapshot.entry);
    historyPlayed = false;
    openingVersion++;
    if (openingController) openingController.abort();
    openingController = null;
    clearInterval(heartbeatTimer);
    heartbeatTimer = null;
    heartbeatPending = false;
    stopStream();
    releaseSession(sessionID, snapshot?.progress, unloading);
    sessionID = '';
    sessionAvailable = false;
    preparedIndex = 0;
    window.JukuPlaybackDanmaku?.close();
  }

  function updateEpisodeControls() {
    node('previousEpisodeBtn').disabled = currentIndex <= 1;
    node('nextEpisodeBtn').disabled = currentIndex < 1 || currentIndex >= episodes.length;
    node('retryPlaybackBtn').disabled = !dramaID && !collectionTaskID;
    episodeButtons.forEach((button, index) => button.setAttribute('aria-current', String(index + 1 === currentIndex)));
    node('playbackEpisodeCount').textContent = episodes.length ? '第 ' + (episodes[currentIndex - 1]?.episode || currentIndex || '—') + ' 集 / 共 ' + episodes.length + ' 集' : '选集';
    const active = episodeButtons[currentIndex - 1];
    if (active) {
      const item = active.getBoundingClientRect();
      const viewport = episodeList.getBoundingClientRect();
      if (item.top < viewport.top) episodeList.scrollTop -= viewport.top - item.top;
      else if (item.bottom > viewport.bottom) episodeList.scrollTop += item.bottom - viewport.bottom;
    }
  }

  function showError(error) {
    window.JukuPlaybackDanmaku?.suspend();
    loading = false;
    errorText.textContent = (error.message || String(error)) + '；可点击“重试播放”。';
    statusText.textContent = '播放未完成';
    if (error.status === 410) sessionAvailable = false;
    updateEpisodeControls();
  }

  function updateDependency(state, text) {
    if (!panel.open || !openingController || sessionID || errorText.textContent) return;
    if (state.status === 'downloading' || state.status === 'verifying') statusText.textContent = text;
    else if (state.status === 'ready') statusText.textContent = collectionMode ? '正在读取合集分集…' : '正在获取分集…';
  }

  function renderEpisodes() {
    clear(episodeList);
    episodeButtons = episodes.map(episode => {
      const button = document.createElement('button');
      button.className = 'secondary';
      button.textContent = '第' + episode.episode + '集';
      button.title = episode.title || button.textContent;
      button.addEventListener('click', () => playEpisode(episode.index));
      episodeList.appendChild(button);
      return button;
    });
    updateEpisodeControls();
  }

  async function heartbeat() {
    if (!sessionID || heartbeatPending) return;
    const currentSession = sessionID;
    heartbeatPending = true;
    try {
      const state = await requestJSON('/api/ui/playback/control', {session: currentSession, action: 'heartbeat'}, openingController?.signal);
      if (currentSession === sessionID && state.run === playbackRun) renderPrefetchStatus(state.prefetch);
    } catch (error) {
      if (currentSession === sessionID && error.status === 410) {
        sessionAvailable = false;
        clearInterval(heartbeatTimer);
        showError(error);
      }
    } finally {
      if (currentSession === sessionID) heartbeatPending = false;
    }
  }

  async function open(id, title, initialIndex = 0, offset = 0, taskID = '', resume = true, fromHistory = false) {
    dispose();
    dramaID = id;
    dramaName = title;
    collectionTaskID = taskID;
    collectionMode = Boolean(taskID);
    episodes = [];
    episodeButtons = [];
    currentIndex = 0;
    lastPosition = offset;
    historySource = '';
    historySequence = 0;
    historyMessage = '';
    historySentAt = 0;
    historyStatus.textContent = '';
    node('playerTitle').textContent = title;
    statusText.textContent = collectionMode ? '正在读取合集分集…' : '正在获取分集…';
    updatePlaybackHint('');
    errorText.textContent = '';
    clear(episodeList);
    updateEpisodeControls();
    if (!panel.open) panel.showModal();
    if (!window.MediaSource || !MediaSource.isTypeSupported('video/mp4; codecs="avc1.42C01F, mp4a.40.2"')) {
      showError(new Error('当前浏览器不支持此在线播放格式，请使用新版 Chrome、Edge、Firefox 或桌面 Safari'));
      return;
    }
    const version = openingVersion;
    openingController = new AbortController();
    try {
      const shouldResume = resume && initialIndex === 0;
      const result = await requestJSON('/api/ui/playback/open', {...(taskID ? {taskId: taskID} : {dramaId: id}), resume: shouldResume, fromHistory}, openingController.signal);
      if (version !== openingVersion || !panel.open) {
        releaseSession(result.session);
        return;
      }
      sessionID = result.session;
      sessionAvailable = true;
      mimeType = result.mimeType;
      dramaID = result.dramaId || id;
      dramaName = result.title || title;
      historySource = result.source || '';
      collectionMode = result.mode === 'collection';
      episodes = result.episodes || [];
      if (!episodes.length || !MediaSource.isTypeSupported(mimeType)) throw new Error('站点没有可播放的分集或浏览器不支持此格式');
      node('playerTitle').textContent = result.title || title;
      renderEpisodes();
      heartbeatTimer = setInterval(heartbeat, 20000);
      historyMessage = shouldResume ? result.resumeMessage || '' : '';
      historyStatus.textContent = historyMessage;
      playEpisode(Math.min(Math.max(initialIndex || result.initialIndex || 1, 1), episodes.length), shouldResume ? Number(result.initialPosition) || 0 : offset, !shouldResume || !result.resumePaused, true);
    } catch (error) {
      if (version === openingVersion && error.name !== 'AbortError') showError(error);
    }
  }

  function openCollection(taskID, title, resume = false) {
    return open('', title, 0, 0, taskID, resume);
  }

  function openHistory(id, title) {
    return open(id, title, 0, 0, '', true, true);
  }

  function reopen(index, offset) {
    if (collectionMode) return open('', dramaName, 0, offset, episodes[index - 1]?.taskId || collectionTaskID, false);
    return open(dramaID, dramaName, index, offset, '', false);
  }

  function updatePlaybackHint(source) {
    if (!collectionMode) {
      node('playbackHint').textContent = '直接观看，不加入下载任务。开启预缓存后，临近播完时提前准备下一集；关闭窗口即停止取流并释放缓存。';
    } else {
      const prefix = source === 'local' ? '本集播放本地已完成文件。' : source === 'online' ? '本集在线缓冲，同时使用原下载任务保存视频。' : '已完成分集优先播放本地，播到未完成分集时自动下载该集。';
      node('playbackHint').textContent = prefix + '只补下载播到的分集；关闭播放器不取消下载，可在下载合集中暂停或取消。';
    }
  }

  function abortError() {
    return new DOMException('播放已停止', 'AbortError');
  }

  function waitForEvent(target, eventName, signal, action) {
    return new Promise((resolve, reject) => {
      function cleanup() {
        target.removeEventListener(eventName, done);
        target.removeEventListener('error', failed);
        signal.removeEventListener('abort', aborted);
      }
      function done() {cleanup(); resolve();}
      function failed() {cleanup(); reject(new Error('浏览器无法解码此视频流'));}
      function aborted() {cleanup(); reject(abortError());}
      if (signal.aborted) {aborted(); return;}
      target.addEventListener(eventName, done, {once: true});
      target.addEventListener('error', failed, {once: true});
      signal.addEventListener('abort', aborted, {once: true});
      try {if (action) action();} catch (error) {cleanup(); reject(error);}
    });
  }

  function bufferedAhead() {
    for (let index = 0; index < video.buffered.length; index++) {
      if (video.currentTime >= video.buffered.start(index) && video.currentTime <= video.buffered.end(index)) return video.buffered.end(index) - video.currentTime;
    }
    return 0;
  }

  function renderPrefetchStatus(view) {
    if (!prefetchToggle.checked || !view || view.episode !== currentIndex + 1) return;
    prefetchStatus.hidden = false;
    prefetchStatus.textContent = view.state === 'ready' ? '下一集已缓存' : view.state === 'failed' ? '下一集将正常缓冲' : '正在缓存下一集…';
  }

  function maybePrefetchNext() {
    if (!prefetchToggle.checked || !streamComplete || loading || video.paused || video.ended || video.seeking || !panel.open || !sessionAvailable || !playbackRun || !currentIndex || currentIndex >= episodes.length || prefetchAttempted === currentIndex) return;
    const remaining = video.duration - video.currentTime;
    if (!Number.isFinite(remaining) || remaining <= 0 || remaining / Math.max(video.playbackRate, 0.25) > 30 || bufferedAhead() < remaining - 0.5) return;
    const session = sessionID;
    const run = playbackRun;
    const version = ++prefetchVersion;
    prefetchAttempted = currentIndex;
    renderPrefetchStatus({episode: currentIndex + 1, state: 'preparing'});
    requestJSON('/api/ui/playback/prefetch', {session, episode: currentIndex + 1, run, version}, openingController?.signal).then(view => {
      if (session === sessionID && run === playbackRun && version === prefetchVersion) renderPrefetchStatus(view);
    }).catch(error => {
      if (session === sessionID && run === playbackRun && version === prefetchVersion && error.name !== 'AbortError') renderPrefetchStatus({episode: currentIndex + 1, state: 'failed'});
    });
  }

  prefetchToggle.addEventListener('change', () => {
    try {localStorage.setItem('juku.playback.prefetchNext', String(prefetchToggle.checked));} catch (_) {}
    prefetchAttempted = 0;
    const version = ++prefetchVersion;
    prefetchStatus.hidden = true;
    if (prefetchToggle.checked) {
      maybePrefetchNext();
    } else if (sessionID && playbackRun) {
      requestJSON('/api/ui/playback/prefetch', {session: sessionID, run: playbackRun, version, cancel: true}, openingController?.signal).catch(() => {});
    }
  });

  async function trimBuffer(buffer, signal) {
    const cutoff = video.currentTime - 20;
    if (cutoff > 0 && buffer.buffered.length && buffer.buffered.start(0) < cutoff) {
      await waitForEvent(buffer, 'updateend', signal, () => buffer.remove(0, cutoff));
    }
  }

  async function playEpisode(index, offset = 0, shouldPlay = true, keepResumeMessage = false) {
    if (!episodes[index - 1]) return;
    if (!sessionAvailable) {reopen(index, offset); return;}
    stopStream();
    if (!keepResumeMessage) {
      historyMessage = '';
      historyStatus.textContent = '';
    }
    currentIndex = index;
    window.JukuPlaybackDanmaku?.setEpisode(sessionID, index, episodes[index - 1].danmaku);
    lastPosition = offset;
    updateEpisodeControls();
    errorText.textContent = '';
    statusText.textContent = offset > 0 ? '正在跳转并缓冲…' : '正在解析播放地址…';
    const version = streamVersion;
    const currentSession = sessionID;
    const controller = new AbortController();
    streamController = controller;
    const signal = controller.signal;
    const source = new MediaSource();
    let reader;
    try {
      if (collectionMode && preparedIndex !== index) {
        statusText.textContent = '正在检查本地分集并准备下载…';
        const preparation = await requestJSON('/api/ui/playback/prepare', {session: currentSession, episode: index}, signal);
        if (signal.aborted) throw abortError();
        preparedIndex = index;
        updatePlaybackHint(preparation.source);
        window.dispatchEvent(new Event('downloadsChanged'));
      }
      objectURL = URL.createObjectURL(source);
      await waitForEvent(source, 'sourceopen', signal, () => {video.src = objectURL;});
      const response = await fetch('/api/ui/playback/stream?' + new URLSearchParams({session: currentSession, episode: String(index), start: String(offset)}), {signal, cache: 'no-store'});
      if (!response.ok) {
        const result = await response.json();
        const error = new Error(result.error || 'HTTP ' + response.status);
        error.status = response.status;
        throw error;
      }
      if (signal.aborted) throw abortError();
      updatePlaybackHint(response.headers.get('X-Playback-Source'));
      const duration = Number(response.headers.get('X-Playback-Duration'));
      playbackDuration = Number.isFinite(duration) && duration > 0 ? duration : 0;
      const run = Number(response.headers.get('X-Playback-Run'));
      playbackRun = run;
      const buffer = source.addSourceBuffer(mimeType);
      buffer.timestampOffset = offset;
      if (duration > 0 && Number.isFinite(duration)) source.duration = duration;
      statusText.textContent = response.headers.get('X-Playback-Prefetched') === '1' ? '正在读取预缓存…' : '正在缓冲…';
      reader = response.body.getReader();
      let initialized = false;
      while (!signal.aborted) {
        while (!signal.aborted && bufferedAhead() > (video.paused && initialized ? 5 : 30)) {
          await new Promise(resolve => setTimeout(resolve, 200));
        }
        if (signal.aborted) throw abortError();
        const chunk = await reader.read();
        if (chunk.done) break;
        await trimBuffer(buffer, signal);
        await waitForEvent(buffer, 'updateend', signal, () => buffer.appendBuffer(chunk.value));
        if (!initialized && buffer.buffered.length) {
          initialized = true;
          video.currentTime = Math.min(buffer.buffered.end(0) - 0.001, Math.max(offset, buffer.buffered.start(0) + 0.03));
          video.playbackRate = Number(node('playbackRate').value) || 1;
          loading = false;
          statusText.textContent = shouldPlay ? '正在播放' : '已暂停';
          if (shouldPlay) video.play().catch(error => {
            if (version !== streamVersion || signal.aborted) return;
            if (error.name === 'NotAllowedError') statusText.textContent = '已就绪，点击视频中的播放按钮';
            else if (error.name !== 'AbortError') showError(error);
          });
        }
      }
      if (signal.aborted) throw abortError();
      const state = await requestJSON('/api/ui/playback/status?session=' + encodeURIComponent(currentSession), undefined, signal);
      if (signal.aborted || version !== streamVersion) throw abortError();
      if (state.run !== run || state.state !== 'ended') throw new Error(state.error || '视频连接中断，请重试');
      if (!initialized) throw new Error('未收到可播放的视频画面');
      if (source.readyState === 'open') source.endOfStream();
      streamComplete = true;
      maybePrefetchNext();
    } catch (error) {
      if (reader) await reader.cancel().catch(() => {});
      if (version === streamVersion && !signal.aborted) showError(error);
    } finally {
      if (reader) reader.releaseLock();
    }
  }

  video.addEventListener('seeking', () => {
    if (loading || !currentIndex || !Number.isFinite(video.currentTime)) return;
    const target = video.currentTime;
    clearTimeout(seekTimer);
    for (let index = 0; index < video.buffered.length; index++) {
      if (target >= video.buffered.start(index) && target < video.buffered.end(index)) return;
    }
    seekTimer = setTimeout(() => playEpisode(currentIndex, target, !video.paused), 180);
  });
  video.addEventListener('timeupdate', () => {if (!loading && Number.isFinite(video.currentTime)) {lastPosition = video.currentTime; if (!video.paused && !video.seeking) saveHistory();} maybePrefetchNext();});
  video.addEventListener('playing', () => {if (!loading && !errorText.textContent) {historyPlayed = true; historyCompleted = false; statusText.textContent = '正在播放'; saveHistory(true);} maybePrefetchNext();});
  video.addEventListener('waiting', () => {if (!loading && !errorText.textContent) statusText.textContent = '正在缓冲…';});
  video.addEventListener('pause', () => {if (!loading) saveHistory(true); if (!loading && !video.ended && !errorText.textContent) statusText.textContent = '已暂停';});
  video.addEventListener('error', () => {
    if (!video.error || !video.hasAttribute('src') || !panel.open) return;
    if (streamController) streamController.abort();
    showError(new Error('浏览器播放失败，请检查网络或重试（错误 ' + video.error.code + '）'));
  });
  video.addEventListener('ended', () => {
    if (loading || !panel.open || errorText.textContent) return;
    historyCompleted = true;
    saveHistory(true);
    if (node('autoNextEpisode').checked && currentIndex < episodes.length) playEpisode(currentIndex + 1);
    else statusText.textContent = '本集播放完毕';
  });
  node('previousEpisodeBtn').addEventListener('click', () => playEpisode(currentIndex - 1));
  node('nextEpisodeBtn').addEventListener('click', () => playEpisode(currentIndex + 1));
  node('retryPlaybackBtn').addEventListener('click', () => {
    preparedIndex = 0;
    if (episodes.length && sessionAvailable) playEpisode(currentIndex || 1, lastPosition);
    else reopen(currentIndex || 1, lastPosition);
  });
  node('playbackRate').addEventListener('change', () => {video.playbackRate = Number(node('playbackRate').value) || 1;});
  node('closePlayerBtn').addEventListener('click', () => panel.close());
  panel.addEventListener('close', () => {if (!panel.open) dispose();});
  document.addEventListener('visibilitychange', () => {if (document.hidden) saveHistory(true);});
  window.addEventListener('pagehide', () => dispose(true));
  window.dramaPlayer = {open, openCollection, openHistory, updateDependency};
})();
