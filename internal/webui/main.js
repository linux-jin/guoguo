import { api, post, sourceLabel, normalizeSource, setMessage } from './ui-core.js';
import { createShell } from './shell.js';
import { createLibrary } from './library.js';
import { createDownloads } from './downloads.js';
import { createSettings } from './settings.js';
import { createFollowing } from './following.js';
import { createDetails } from './details.js';

const app = {api, post};
app.play = (id, title) => {
  if (window.JukuHistory.get(id)) window.dramaPlayer.openHistory(id, title);
  else window.dramaPlayer.open(id, title);
  app.following.acknowledge(id);
};
app.shell = createShell(app);
app.library = createLibrary(app);
app.downloads = createDownloads(app);
app.settings = createSettings(app);
app.following = createFollowing(app);
app.details = createDetails(app);

app.shell.init();
window.JukuHistory.init({
  api, post,
  sourceLabel: value => sourceLabel(normalizeSource(value) || value),
  onChanged: () => {app.library.refreshFollowing(); app.following.render(); app.details.refresh(); app.downloads.render();},
  onError: message => setMessage(message, true),
  play: app.play
});
window.JukuRankings.init({
  api, post,
  getSource: () => document.getElementById('sourceSelect').value,
  sourceLabel,
  onLibraryChanged: () => app.library.refresh(),
  onDownloadsChanged: app.downloads.refresh,
  play: app.play
});
Promise.allSettled([app.settings.init(), app.library.init(), app.downloads.init(), app.following.init()]).then(results => {
  const failure = results.find(result => result.status === 'rejected');
  if (failure) setMessage('部分功能未能初始化：' + (failure.reason?.message || '请刷新页面重试'), true);
  document.documentElement.dataset.ready = 'true';
});
