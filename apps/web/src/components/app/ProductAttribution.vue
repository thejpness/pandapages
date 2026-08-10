<template>
  <p class="product-attribution">
    Panda Pages is a
    <a
      class="product-attribution__link"
      href="https://southcoastapps.co.uk"
      target="_blank"
      rel="noreferrer"
    >South Coast Apps</a>
    product
  </p>
</template>

<style scoped>
.product-attribution {
  margin: 0;
  color: var(--panda-muted, var(--soft-ink));
  font-size: 0.76rem;
  line-height: 1.5;
}

.product-attribution__link {
  display: inline-flex;
  min-height: 2.75rem;
  align-items: center;
  margin-inline: -0.12rem;
  padding-inline: 0.12rem;
  color: inherit;
  font-weight: 750;
  text-decoration: underline;
  text-decoration-thickness: 0.08em;
  text-underline-offset: 0.16em;
}

.product-attribution__link:focus-visible {
  outline: 3px solid var(--panda-focus, var(--ink));
  outline-offset: 2px;
}

@media (forced-colors: active) {
  .product-attribution__link {
    color: LinkText;
  }
}
</style>
