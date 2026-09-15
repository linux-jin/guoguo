import { $, element, button, icon, initial, withFocus, dramaTitle, sourceKey, sourceLabel, categoryName, episodeCount, firstNonEmpty, tagsText, releaseText, setMessage } from './ui-core.js';

export function createDetails(app) {
  let currentID = '';
  const panel = $('detailPanel');
  const content = $('detailContent');

  function render() {
    if (!currentID) return;
    const saved = app.following.get(currentID);
    const history = window.JukuHistory.get(currentID);
    const known = app.library.get(currentID);
    const drama = known || {id: currentID, title: saved?.title || history?.title || '短剧', source: saved?.source || history?.source, totalEpisode: saved?.totalEpisode || history?.total, categoryName: saved?.category};
    const title = dramaTitle(drama);
    withFocus(content, () => {
      content.replaceChildren();
      const top = element('div', 'detail-top');
      const identity = element('div', 'detail-initial', initial(title));
      identity.setAttribute('aria-hidden', 'true');
      const heading = element('div', 'spacer');
      const name = element('h1', '', title);
      name.id = 'detailTitle';
      heading.append(name, element('p', 'detail-meta', sourceLabel(sourceKey(drama)) + ' · ' + categoryName(drama)));
      const tags = element('div', 'tags detail-tags');
      tagsText(drama).slice(0, 10).forEach(tag => tags.appendChild(element('span', 'tag', tag)));
      heading.appendChild(tags);
      top.append(identity, heading);
      content.appendChild(top);
      if (history) content.appendChild(element('p', 'detail-progress', window.JukuHistory.progressText(history)));
      if (saved?.newEpisodes > 0) content.appendChild(element('p', 'following-update notice', '剧库新增 ' + saved.newEpisodes + ' 集'));
      const actions = element('div', 'detail-actions');
      const play = button(history ? '继续观看' : '开始观看', () => app.play(currentID, title), false, 'primary-action button-content');
      play.prepend(icon('play'));
      play.dataset.focusKey = 'detail-play';
      const bookmark = button(saved?.saved ? '已收藏' : '加入想看', () => app.following.toggleSaved(currentID), app.following.busy(currentID), 'secondary');
      bookmark.id = 'detailSaveBtn';
      bookmark.dataset.focusKey = 'detail-save';
      bookmark.setAttribute('aria-pressed', String(Boolean(saved?.saved)));
      actions.append(play, bookmark);
      content.appendChild(actions);
      content.appendChild(element('p', 'detail-description', firstNonEmpty(drama.desc, drama.intro) || '站源暂未提供简介。'));
      const facts = element('dl', 'detail-facts');
      const values = [['集数', episodeCount(drama) ? episodeCount(drama) + ' 集' : '暂未提供'], ['状态', releaseText(drama.releaseStatus)], ['上线时间', drama.onlineDate], ['站点热度', drama.heat], ['播放量', drama.views], ['站点评分', drama.score]];
      for (const [label, value] of values) {
        if (!value) continue;
        const item = element('div');
        item.append(element('dt', '', label), element('dd', '', value));
        facts.appendChild(item);
      }
      content.appendChild(facts);
      const secondary = element('div', 'detail-secondary');
      const download = button('加入下载', () => app.downloads.enqueue([currentID]), !known, 'secondary');
      download.id = 'detailDownloadBtn';
      download.dataset.focusKey = 'detail-download';
      if (!known) download.title = '更新剧库找到此剧后可加入下载';
      const watched = button(saved?.completed ? '取消已看标记' : '标为已看', () => app.following.setCompleted(currentID, !saved?.completed), app.following.busy(currentID), 'secondary');
      watched.id = 'detailCompletedBtn';
      watched.dataset.focusKey = 'detail-completed';
      secondary.append(download, watched);
      actions.after(secondary);
      content.appendChild(element('p', 'small notice', '手动标记用于整理清单，实际播放进度仍自动保存。'));
    });
  }

  function open(id) {
    currentID = id;
    render();
    window.JukuDialogs.open(panel);
  }

  return {open, refresh: () => {if (panel.open) render();}};
}
