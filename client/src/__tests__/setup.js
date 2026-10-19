import '@testing-library/jest-dom'

// jsdom não tem IntersectionObserver; o leitor de mangá usa para saber a página visível.
if (!globalThis.IntersectionObserver) {
  globalThis.IntersectionObserver = class {
    observe() {}
    unobserve() {}
    disconnect() {}
    takeRecords() { return [] }
  }
}
