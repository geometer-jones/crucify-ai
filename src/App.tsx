import { useState } from 'react';
import { essays } from './data/essays';

function App() {
  const [selectedEssayId, setSelectedEssayId] = useState(essays[0]?.id ?? '');

  const selectedEssay =
    essays.find((essay) => essay.id === selectedEssayId) ?? essays[0];

  return (
    <div className="page-shell">
      <header className="hero">
        <p className="eyebrow">Essay Collection</p>
        <h1>Crucify AI</h1>
        <p className="hero-copy">
          A living collection about religion, technology, and the structures
          we choose to remember.
        </p>
      </header>

      <main className="layout">
        <aside className="essay-list" aria-label="Essay collection">
          <div className="section-heading">
            <h2>Collection</h2>
            <span>{essays.length} essays</span>
          </div>

          {essays.map((essay) => {
            const isActive = essay.id === selectedEssay?.id;

            return (
              <button
                key={essay.id}
                type="button"
                className={`essay-card${isActive ? ' active' : ''}`}
                onClick={() => setSelectedEssayId(essay.id)}
              >
                <span className="essay-meta">
                  {essay.category} · {essay.readTime}
                </span>
                <strong>{essay.title}</strong>
                <p>{essay.excerpt}</p>
                <span className="essay-date">{essay.publishedAt}</span>
              </button>
            );
          })}
        </aside>

        <article className="reader">
          <div className="reader-header">
            <p className="eyebrow">{selectedEssay.category}</p>
            <h2>{selectedEssay.title}</h2>
            <div className="reader-meta">
              <span>{selectedEssay.publishedAt}</span>
              <span>{selectedEssay.readTime}</span>
            </div>
            <p className="reader-excerpt">{selectedEssay.excerpt}</p>
          </div>

          <div className="reader-body">
            {selectedEssay.content.map((paragraph) => (
              <p key={paragraph}>{paragraph}</p>
            ))}
          </div>
        </article>
      </main>
    </div>
  );
}

export default App;
