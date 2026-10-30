import { describe, it, expect } from 'vitest';
import '@testing-library/jest-dom';
import { render, screen } from '@testing-library/react';
import { Field } from '../Field';

function renderField(props: Partial<Parameters<typeof Field>[0]> = {}) {
  return render(
    <Field label="Name" {...props}>
      {({ id, describedBy, invalid }) => (
        <input id={id} aria-describedby={describedBy} aria-invalid={invalid} />
      )}
    </Field>,
  );
}

describe('Field', () => {
  it('binds the label to the control', () => {
    renderField();
    const input = screen.getByLabelText('Name');
    const label = screen.getByText('Name').closest('label');
    expect(label).toHaveAttribute('for', input.id);
    expect(input).toHaveAttribute('id', input.id);
  });

  it('describes the control by help, error, or both', () => {
    const { rerender } = render(
      <Field label="Name" help="Kebab-case">
        {({ id, describedBy, invalid }) => (
          <input id={id} aria-describedby={describedBy} aria-invalid={invalid} />
        )}
      </Field>,
    );
    const helpOnly = screen.getByLabelText('Name');
    const helpId = helpOnly.getAttribute('aria-describedby');
    expect(helpId).toBeTruthy();
    expect(document.getElementById(helpId!)).toHaveTextContent('Kebab-case');
    expect(helpOnly).not.toHaveAttribute('aria-invalid');

    rerender(
      <Field label="Name" error="Name is required">
        {({ id, describedBy, invalid }) => (
          <input id={id} aria-describedby={describedBy} aria-invalid={invalid} />
        )}
      </Field>,
    );
    const errorOnly = screen.getByLabelText(/^Name/);
    expect(errorOnly).toHaveAttribute('aria-invalid', 'true');
    const errorId = errorOnly.getAttribute('aria-describedby');
    expect(document.getElementById(errorId!)).toHaveTextContent('Name is required');

    rerender(
      <Field label="Name" help="Kebab-case" error="Name is required">
        {({ id, describedBy, invalid }) => (
          <input id={id} aria-describedby={describedBy} aria-invalid={invalid} />
        )}
      </Field>,
    );
    const both = screen.getByLabelText(/^Name/);
    const ids = both.getAttribute('aria-describedby')!.split(' ');
    expect(ids).toHaveLength(2);
    const texts = ids.map((describedId) => document.getElementById(describedId)?.textContent);
    expect(texts.join(' ')).toContain('Name is required');
    expect(texts.join(' ')).toContain('Kebab-case');
  });

  it('puts the required marker in a status span with the space outside it', () => {
    renderField({ required: true });
    const label = screen.getByText(/^Name/).closest('label')!;
    expect(label).toHaveTextContent('Name *');
    const star = label.querySelector('span');
    expect(star).toHaveClass('text-status-error');
    expect(star).toHaveTextContent('*');
    expect(star?.textContent).toBe('*');
    expect(label.childNodes[0].textContent).toBe('Name');
    expect(label.childNodes[1].textContent).toBe(' ');
  });
});
