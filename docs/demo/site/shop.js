// Acme Mugs cart. The bug the demo finds: dataset values are strings.
let total = 0;
let count = 0;

for (const button of document.querySelectorAll("button[data-price]")) {
  button.addEventListener("click", () => addToCart(button));
}

function addToCart(button) {
  const price = button.dataset.price;
  total = total + price;
  count += 1;
  render();
}

function render() {
  document.getElementById("count").textContent = count;
  document.getElementById("total").textContent = "$" + total;
}

formatDate(new Date());
