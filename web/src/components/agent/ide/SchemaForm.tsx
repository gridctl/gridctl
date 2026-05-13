import Form from '@rjsf/core';
import validator from '@rjsf/validator-ajv8';
import type { RJSFSchema } from '@rjsf/utils';

interface SchemaFormProps {
  schema: Record<string, unknown>;
  value: Record<string, unknown>;
  onChange: (next: Record<string, unknown>) => void;
}

/**
 * SchemaForm wraps RJSF with our minimal styling. Lazy-loaded by
 * RunLauncherModal so the ~90KB RJSF + AJV chunk only ships when the
 * Form tab is actually opened — which today only happens for skills
 * whose `inputSchema` exposes properties beyond `{type:"object"}`
 * (none in v1; future TS-schema extraction will unlock it).
 *
 * The Submit button is suppressed — the parent modal owns the Run
 * action and reads form values back through onChange.
 */
export default function SchemaForm({ schema, value, onChange }: SchemaFormProps) {
  return (
    <div className="rjsf-launcher font-sans text-sm">
      <Form
        schema={schema as RJSFSchema}
        formData={value}
        validator={validator}
        onChange={(e) => {
          const next = (e.formData ?? {}) as Record<string, unknown>;
          onChange(next);
        }}
        liveValidate={false}
        showErrorList={false}
      >
        {/* Empty children suppresses RJSF's default submit button;
            the parent modal owns the Run action. */}
        <span />
      </Form>
    </div>
  );
}
