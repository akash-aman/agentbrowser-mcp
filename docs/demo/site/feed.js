// Acme Journal. The problems the demo finds: a banner that pushes the posts
// down after load, and a scroll handler that re-sorts 600k numbers each time.
const titles = ["Pour-over, step by step", "Why glaze matters", "The perfect handle", "Cold brew at home",
  "Mugs we retired", "Desk setups from readers", "A short history of the cup", "Cleaning tea stains",
  "Espresso cups, ranked", "Travel mugs that seal", "Clay, kilns and patience", "Our studio in winter"];
const feed = document.getElementById("feed");
for (const [i, title] of titles.entries()) {
  const post = document.createElement("article");
  post.className = "post";
  post.innerHTML = `<h2>${title}</h2><p>Post ${i + 1}. Notes from the studio on making, using and caring for the mugs on your desk, with photos from readers.</p>`;
  feed.append(post);
}

// The promo "loads" late and lands above the posts.
setTimeout(() => {
  const promo = document.createElement("aside");
  promo.className = "promo";
  promo.textContent = "Autumn sale: 20% off every mug this week";
  feed.prepend(promo);
}, 900);

const scores = Array.from({ length: 600000 }, (_, i) => Math.sin(i));
function rankPosts() {
  return [...scores].sort((a, b) => b - a).slice(0, 5);
}

addEventListener("scroll", onScroll);
function onScroll() {
  rankPosts();
  const max = document.documentElement.scrollHeight - innerHeight;
  document.getElementById("progress").style.width = (scrollY / max) * 100 + "%";
  for (const post of document.querySelectorAll(".post")) {
    post.style.boxShadow = `0 ${4 + (scrollY % 12)}px 24px rgba(0, 0, 0, .12)`;
  }
}

// The ticker moves with left, which repaints on every frame.
const ticker = document.getElementById("ticker");
(function slide(t) {
  ticker.style.left = 20 + Math.sin(t / 600) * 20 + "px";
  requestAnimationFrame(slide);
})(0);
