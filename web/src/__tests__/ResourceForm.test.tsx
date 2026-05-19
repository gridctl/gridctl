import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, fireEvent } from '@testing-library/react';
import '@testing-library/jest-dom';
import { ResourceForm } from '../components/wizard/steps/ResourceForm';
import type { ResourceFormData } from '../lib/yaml-builder';

function defaultData(overrides?: Partial<ResourceFormData>): ResourceFormData {
  return { name: '', image: '', ...overrides };
}

const noop = (_data: Partial<ResourceFormData>) => {};

describe('ResourceForm', () => {
  let onChange: typeof noop;

  beforeEach(() => {
    onChange = vi.fn<typeof noop>();
  });

  it('renders all 6 accordion sections', () => {
    render(<ResourceForm data={defaultData()} onChange={onChange} />);
    expect(screen.getByText('Preset')).toBeInTheDocument();
    expect(screen.getByText('Identity')).toBeInTheDocument();
    expect(screen.getByText('Environment & Secrets')).toBeInTheDocument();
    expect(screen.getByText('Ports')).toBeInTheDocument();
    expect(screen.getByText('Volumes')).toBeInTheDocument();
    expect(screen.getByText('Network')).toBeInTheDocument();
  });

  it('renders 4 preset options plus custom', () => {
    render(<ResourceForm data={defaultData()} onChange={onChange} />);
    expect(screen.getByText('PostgreSQL')).toBeInTheDocument();
    expect(screen.getByText('Redis')).toBeInTheDocument();
    expect(screen.getByText('MySQL')).toBeInTheDocument();
    expect(screen.getByText('MongoDB')).toBeInTheDocument();
    expect(screen.getByText('Custom')).toBeInTheDocument();
  });

  it('enforces kebab-case on name input', () => {
    render(<ResourceForm data={defaultData()} onChange={onChange} />);
    const nameInput = screen.getByPlaceholderText('my-resource');
    fireEvent.change(nameInput, { target: { value: 'My Resource' } });
    expect(onChange).toHaveBeenCalledWith({ name: 'my-resource' });
  });

  it('pre-fills form when selecting PostgreSQL preset', () => {
    render(<ResourceForm data={defaultData()} onChange={onChange} />);
    fireEvent.click(screen.getByText('PostgreSQL'));
    expect(onChange).toHaveBeenCalledWith(
      expect.objectContaining({
        name: 'postgres',
        image: 'postgres:16',
        ports: ['5432:5432'],
      }),
    );
  });

  it('pre-fills form when selecting Redis preset', () => {
    render(<ResourceForm data={defaultData()} onChange={onChange} />);
    fireEvent.click(screen.getByText('Redis'));
    expect(onChange).toHaveBeenCalledWith(
      expect.objectContaining({
        name: 'redis',
        image: 'redis:7-alpine',
        ports: ['6379:6379'],
      }),
    );
  });

  it('pre-fills form when selecting MySQL preset', () => {
    render(<ResourceForm data={defaultData()} onChange={onChange} />);
    fireEvent.click(screen.getByText('MySQL'));
    expect(onChange).toHaveBeenCalledWith(
      expect.objectContaining({
        name: 'mysql',
        image: 'mysql:8',
        ports: ['3306:3306'],
      }),
    );
  });

  it('pre-fills form when selecting MongoDB preset', () => {
    render(<ResourceForm data={defaultData()} onChange={onChange} />);
    fireEvent.click(screen.getByText('MongoDB'));
    expect(onChange).toHaveBeenCalledWith(
      expect.objectContaining({
        name: 'mongodb',
        image: 'mongo:7',
        ports: ['27017:27017'],
      }),
    );
  });

  it('resets form when selecting Custom', () => {
    render(<ResourceForm data={defaultData()} onChange={onChange} />);
    fireEvent.click(screen.getByText('Custom'));
    expect(onChange).toHaveBeenCalledWith({ name: '', image: '' });
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

  it('includes vault references in preset env vars', () => {
    render(<ResourceForm data={defaultData()} onChange={onChange} />);
    fireEvent.click(screen.getByText('PostgreSQL'));
    expect(onChange).toHaveBeenCalledWith(
      expect.objectContaining({
        env: expect.objectContaining({
          POSTGRES_PASSWORD: '${var:POSTGRES_PASSWORD}',
        }),
      }),
    );
  });
});
