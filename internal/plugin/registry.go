package plugin

import (
	"fmt"
	"log"
	"sync"

	pb "github.com/fipso/chadbot/gen/chadbot"
)

// RegisteredSkill holds a skill and its owner plugin
type RegisteredSkill struct {
	Skill      *pb.Skill
	PluginID   string
	PluginName string
}

// Registry manages skill registration and lookup
type Registry struct {
	mu     sync.RWMutex
	skills map[string]*RegisteredSkill
}

// NewRegistry creates a new skill registry
func NewRegistry() *Registry {
	return &Registry{
		skills: make(map[string]*RegisteredSkill),
	}
}

// RegisterSkill registers a skill for a plugin
func (r *Registry) RegisterSkill(pluginID, pluginName string, skill *pb.Skill) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if existing, ok := r.skills[skill.Name]; ok {
		return fmt.Errorf("skill %q already registered by plugin %s", skill.Name, existing.PluginID)
	}

	r.skills[skill.Name] = &RegisteredSkill{
		Skill:      skill,
		PluginID:   pluginID,
		PluginName: pluginName,
	}

	log.Printf("[Registry] Registered skill: %s (plugin: %s)", skill.Name, pluginName)
	return nil
}

// UnregisterPluginSkills removes all skills for a plugin
func (r *Registry) UnregisterPluginSkills(pluginID string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	for name, rs := range r.skills {
		if rs.PluginID == pluginID {
			delete(r.skills, name)
			log.Printf("[Registry] Unregistered skill: %s (plugin: %s)", name, pluginID)
		}
	}
}

// GetSkill returns a registered skill by name
func (r *Registry) GetSkill(name string) (*RegisteredSkill, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	skill, ok := r.skills[name]
	return skill, ok
}

// ListSkills returns all registered skills
func (r *Registry) ListSkills() []*RegisteredSkill {
	r.mu.RLock()
	defer r.mu.RUnlock()

	skills := make([]*RegisteredSkill, 0, len(r.skills))
	for _, rs := range r.skills {
		skills = append(skills, rs)
	}
	return skills
}

// GetSkillsForLLM returns skills formatted for LLM function calling
func (r *Registry) GetSkillsForLLM() []*pb.Skill {
	r.mu.RLock()
	defer r.mu.RUnlock()

	skills := make([]*pb.Skill, 0, len(r.skills))
	for _, rs := range r.skills {
		skills = append(skills, rs.Skill)
	}
	return skills
}

// GetPluginsWithSkills returns unique plugin names that have registered skills
func (r *Registry) GetPluginsWithSkills() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	seen := make(map[string]bool)
	var names []string
	for _, rs := range r.skills {
		if !seen[rs.PluginName] {
			seen[rs.PluginName] = true
			names = append(names, rs.PluginName)
		}
	}
	return names
}
