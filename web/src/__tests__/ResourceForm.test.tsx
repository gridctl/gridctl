import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, fireEvent } from '@testing-library/react';
import '@testing-library/jest-dom';
import { ResourceForm } from '../components/wizard/steps/ResourceForm';
import type { ResourceFormData } from '../lib/yaml-builder';

function defaultData(overrides?: Partial<ResourceFormData>): ResourceFormData {
  return { name: '', image: '', ...overrides };
}

type OnChange = (data: Partial<ResourceFormData>) => void;

describe('ResourceForm', () => {
  let onChange: OnChange;

  beforeEach(() => {
    onChange = vi.fn<OnChange>();
  });

  it('renders all 5 accordion sections', () => {
    render(<ResourceForm data={defaultData()} onChange={onChange} />);
    expect(screen.queryByText('Preset')).not.toBeInTheDocument();
    expect(screen.getByText('Identity')).toBeInTheDocument();
    expect(screen.getByText('Environment & Variables')).toBeInTheDocument();
    expect(screen.getByText('Ports')).toBeInTheDocument();
    expect(screen.getByText('Volumes')).toBeInTheDocument();
    expect(screen.getByText('Network')).toBeInTheDocument();
  });

  it('enforces kebab-case on name input', () => {
    render(<ResourceForm data={defaultData()} onChange={onChange} />);
    const nameInput = screen.getByPlaceholderText('my-resource');
    fireEvent.change(nameInput, { target: { value: 'My Resource' } });
    expect(onChange).toHaveBeenCalledWith({ name: 'my-resource' });
  });

  it('shows image field', () => {
    render(<ResourceForm data={defaultData()} onChange={onChange} />);
    expect(screen.getByPlaceholderText('postgres:16')).toBeInTheDocument();
  });

  it('displays field errors when provided', () => {
    render(
      <ResourceForm
        data={defaultData()}
        onChange={onChange}
        errors={{ name: 'Name is required', image: 'Image is required' }}
      />,
    );
    expect(screen.getByText('Name is required')).toBeInTheDocument();
    expect(screen.getByText('Image is required')).toBeInTheDocument();
  });

  it('shows env badge count when env vars are set', () => {
    render(
      <ResourceForm
        data={defaultData({ env: { DB_HOST: 'localhost' } })}
        onChange={onChange}
      />,
    );
    expect(screen.getByText('1')).toBeInTheDocument();
  });

  it('shows port badge count when ports are set', () => {
    render(
      <ResourceForm
        data={defaultData({ ports: ['5432:5432', '5433:5433'] })}
        onChange={onChange}
      />,
    );
    expect(screen.getByText('2')).toBeInTheDocument();
  });

  it('shows volume badge count when volumes are set', () => {
    render(
      <ResourceForm
        data={defaultData({ volumes: ['./data:/var/lib/data'] })}
        onChange={onChange}
      />,
    );
    // The badge shows "1"
    expect(screen.getByText('1')).toBeInTheDocument();
  });

  it('shows network field', () => {
    render(<ResourceForm data={defaultData()} onChange={onChange} />);
    fireEvent.click(screen.getByText('Network'));
    expect(screen.getByPlaceholderText('network-name')).toBeInTheDocument();
  });
});
